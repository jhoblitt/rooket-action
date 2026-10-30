package tools

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Retry bounds the requests Install makes for one pin.
type Retry struct {
	// Attempts is the most requests made; below 1 counts as 1.
	Attempts int
	// PerRequest caps one attempt, the body's download included; zero
	// means no cap.
	PerRequest time.Duration
	// Backoff is the wait before the second attempt, doubled before each
	// attempt after it.
	Backoff time.Duration
}

// DefaultRetry rides out a CDN hiccup without stalling a job for long. Two
// minutes per attempt is ample for kubectl, the largest pin, on a hosted
// runner's link.
var DefaultRetry = Retry{Attempts: 4, PerRequest: 2 * time.Minute, Backoff: 5 * time.Second}

// Install downloads p, checks it against p.SHA256, and writes the binary to
// binDir/<p.Name> with mode 0755. binDir must exist.
//
// A transport error, a non-2xx status, or an attempt that outlasts
// r.PerRequest is retried, up to r.Attempts in all. A hash mismatch is not
// retried, because the server answered in full with the wrong bytes.
//
// The binary is written to a temporary file in binDir and renamed over its
// destination, so a copy that is running is replaced, never truncated.
func Install(ctx context.Context, c *http.Client, p Pin, binDir string, r Retry) error {
	if err := install(ctx, c, p, binDir, r); err != nil {
		return fmt.Errorf("installing %s: %w", p.Name, err)
	}
	return nil
}

func install(ctx context.Context, c *http.Client, p Pin, binDir string, r Retry) error {
	dl, err := os.CreateTemp(binDir, "."+p.Name+"-*.download")
	if err != nil {
		return err
	}
	err = fetch(ctx, c, p, dl, r)
	if err == nil {
		err = emplace(dl, p.Member, filepath.Join(binDir, p.Name))
	}
	return errors.Join(err, discard(dl))
}

// fetch downloads p.URL into dst, retrying as r allows, and checks the
// download against p.SHA256.
func fetch(ctx context.Context, c *http.Client, p Pin, dst *os.File, r Retry) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, http.NoBody)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", p.URL, err)
	}
	attempts := max(r.Attempts, 1)
	delay := r.Backoff
	for n := 1; ; n++ {
		if err := rewind(dst); err != nil {
			return err
		}
		sum, err := attempt(ctx, c, req, dst, r.PerRequest)
		if err == nil {
			if !strings.EqualFold(sum, p.SHA256) {
				return fmt.Errorf("downloading %s: sha256 mismatch: got %s, want %s", p.URL, sum, p.SHA256)
			}
			return nil
		}
		if n == attempts {
			return fmt.Errorf("downloading %s: attempt %d of %d: %w", p.URL, n, attempts, err)
		}
		if werr := pause(ctx, delay); werr != nil {
			return fmt.Errorf("downloading %s: attempt %d of %d: %w; stopped retrying: %w", p.URL, n, attempts, err, werr)
		}
		slog.WarnContext(ctx, "retrying download",
			slog.String("tool", p.Name),
			slog.String("url", p.URL),
			slog.Int("attempt", n+1),
			slog.Any("error", err))
		delay *= 2
	}
}

// rewind empties f for the next attempt.
func rewind(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.Seek(0, io.SeekStart)
	return err
}

// attempt makes one request, writes the body to dst, and returns the body's
// hex sha256.
func attempt(ctx context.Context, c *http.Client, req *http.Request, dst io.Writer, timeout time.Duration) (sum string, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	resp, err := c.Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(dst, h), resp.Body); err != nil {
		return "", fmt.Errorf("reading body: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// pause waits d, or returns ctx's error as soon as ctx is done.
func pause(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// emplace renames the checked download dl to dst or, when member is set,
// extracts that member of it to dst through a second temporary file.
func emplace(dl *os.File, member, dst string) error {
	if member == "" {
		return place(dl, dst)
	}
	out, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	if err := extract(dl, member, out); err != nil {
		return errors.Join(fmt.Errorf("extracting %s: %w", member, err), discard(out))
	}
	return errors.Join(place(out, dst), discard(out))
}

// extract copies the regular file named member, out of the gzipped tar
// archive f, into dst.
func extract(f *os.File, member string, dst io.Writer) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("not in the archive")
		}
		if err != nil {
			return err
		}
		if path.Clean(hdr.Name) != path.Clean(member) {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("tar type %q is not a regular file", hdr.Typeflag)
		}
		_, err = io.CopyN(dst, tr, hdr.Size)
		return err
	}
}

// place makes f executable, closes it, and renames it to dst. The rename
// gives dst a new inode, so a process running the old dst keeps it intact.
func place(f *os.File, dst string) error {
	if err := f.Chmod(0o755); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dst)
}

// discard closes and removes the temporary file f. A file that is already
// closed, or already renamed into place, is not an error.
func discard(f *os.File) error {
	cerr := f.Close()
	if errors.Is(cerr, os.ErrClosed) {
		cerr = nil
	}
	rerr := os.Remove(f.Name())
	if errors.Is(rerr, fs.ErrNotExist) {
		rerr = nil
	}
	return errors.Join(cerr, rerr)
}
