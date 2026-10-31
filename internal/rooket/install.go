// Package rooket installs the rooket binary the action's inputs choose, and
// reports the version it installed.
package rooket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/jhoblitt/rooket-action/internal/tools"
)

// module is what go install fetches when rooket-version is set alone.
const module = "github.com/jhoblitt/rooket"

var (
	releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)
	hexSHA256      = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// Options are the action's rooket inputs, and where Install puts rooket.
// Version, SHA256 and Path are trimmed of surrounding whitespace, and one
// that is then empty is unset.
type Options struct {
	// Go is the go binary that runs go install. Only UsesGo's case needs it.
	Go string
	// GoModCache is the absolute module cache go install uses. Only UsesGo's
	// case needs it.
	GoModCache string
	// BinDir is the existing, absolute directory rooket is installed into.
	BinDir string
	// Version is the rooket-version input: with SHA256, a release to
	// download; alone, anything go install accepts after the @.
	Version string
	// SHA256 is the rooket-sha256 input, the hex sha256 of Version's
	// linux/amd64 release archive.
	SHA256 string
	// Path is the rooket-path input: a rooket binary to copy in, absolute
	// or relative to the working directory.
	Path string
}

type method int

const (
	download method = iota
	goInstall
	copyFile
)

// source is how Install obtains rooket.
type source struct {
	method method
	// pin is the release download's.
	pin tools.Pin
	// version is go install's query.
	version string
	// path is the file copyFile copies.
	path string
}

// UsesGo reports whether Install runs go install for o, so that o.Go must
// name a go binary. It is false for inputs Install would refuse.
func (o Options) UsesGo() bool {
	src, err := o.source()
	return err == nil && src.method == goInstall
}

// Validate returns the error Install returns for o's inputs before it
// installs anything, or nil.
func (o Options) Validate() error {
	_, err := o.source()
	return err
}

func (o Options) source() (source, error) {
	version := strings.TrimSpace(o.Version)
	sum := strings.TrimSpace(o.SHA256)
	path := strings.TrimSpace(o.Path)
	switch {
	case path != "" && (version != "" || sum != ""):
		var with []string
		if version != "" {
			with = append(with, "rooket-version")
		}
		if sum != "" {
			with = append(with, "rooket-sha256")
		}
		return source{}, fmt.Errorf("input rooket-path excludes %s: rooket-path names a binary to use instead of installing one",
			strings.Join(with, " and "))
	case path != "":
		return source{method: copyFile, path: path}, nil
	case sum != "" && version == "":
		return source{}, errors.New("input rooket-sha256 needs rooket-version: a hash alone names no release")
	case sum != "":
		if !releaseVersion.MatchString(version) {
			return source{}, fmt.Errorf("input rooket-version: %q is not a release version such as v1.2.3, which rooket-sha256 requires", version)
		}
		if !hexSHA256.MatchString(sum) {
			return source{}, fmt.Errorf("input rooket-sha256: %q is not 64 hex digits", sum)
		}
		return source{method: download, pin: tools.Rooket(version, strings.ToLower(sum))}, nil
	case version != "":
		if strings.ContainsFunc(version, unicode.IsSpace) {
			return source{}, fmt.Errorf("input rooket-version: %q contains whitespace", version)
		}
		return source{method: goInstall, version: version}, nil
	default:
		return source{method: download, pin: tools.DefaultRooket()}, nil
	}
}

// Install installs rooket into o.BinDir as o's inputs choose, and returns
// its path, BinDir/rooket:
//
//   - Path set: copies that file in with mode 0755.
//   - Neither Version nor SHA256 set: downloads tools.DefaultRooket()'s
//     archive and checks it against the pinned hash.
//   - Both set: downloads tools.Rooket(Version, SHA256)'s archive and checks
//     it against SHA256. Version must be a release, vX.Y.Z or X.Y.Z, and
//     SHA256 64 hex digits.
//   - Version alone: runs o.Go install github.com/jhoblitt/rooket@Version,
//     with goEnv over whatever the parent sets, so that the checksum
//     database verifies the module and o.GoModCache holds it.
//
// Path with Version or SHA256, and SHA256 alone, are errors. An input error
// names the input at fault, and Install returns it before touching
// anything. Each way replaces a rooket already in BinDir.
func Install(ctx context.Context, r Runner, c *http.Client, retry tools.Retry, o Options) (string, error) {
	src, err := o.source()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(o.BinDir, "rooket")
	switch src.method {
	case copyFile:
		slog.InfoContext(ctx, "copying rooket", slog.String("path", src.path))
		if err := copyExecutable(src.path, dst); err != nil {
			return "", fmt.Errorf("installing rooket: copying %s: %w", src.path, err)
		}
	case goInstall:
		slog.InfoContext(ctx, "installing rooket with go install", slog.String("version", src.version))
		if err := r.Run(ctx, goEnv(o.BinDir, o.GoModCache), o.Go, "install", module+"@"+src.version); err != nil {
			return "", fmt.Errorf("installing rooket: %w", err)
		}
	case download:
		slog.InfoContext(ctx, "downloading rooket", slog.String("version", src.pin.Version))
		if err := tools.Install(ctx, c, src.pin, o.BinDir, retry); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// goEnv is go install's environment over the parent's, with GOBIN=binDir
// and GOMODCACHE=modCache. A module cache of its own keeps rooket's modules
// out of the consumer's: go makes each module it extracts read-only, so a
// later actions/setup-go step fails to restore a cache holding the same
// module over it.
// GOTOOLCHAIN=auto lets a Go older than rooket's go.mod fetch the toolchain
// it names. GOWORK=off and the emptied GOFLAGS, GOOS and GOARCH keep a
// consumer's workspace, flags (-mod=vendor, say) and cross-compilation target
// out of a build that must run on this host. GOSUMDB and the emptied
// GONOSUMDB and GOPRIVATE hold rooket's module to the checksum database,
// which is what verifies it. go reads a variable set to "" as unset and
// falls back to the go env -w file, so GOENV=off is what makes each emptied
// variable stay empty.
func goEnv(binDir, modCache string) []string {
	return []string{
		"GOBIN=" + binDir, "GOMODCACHE=" + modCache, "GOTOOLCHAIN=auto", "GOWORK=off", "GOFLAGS=", "GOOS=", "GOARCH=",
		"GOENV=off", "GOSUMDB=sum.golang.org", "GONOSUMDB=", "GOPRIVATE=",
	}
}

// copyExecutable copies src to dst through tools.WriteExecutable, so src may
// be dst itself.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // G304: src is the rooket-path input, which names the file to copy
	if err != nil {
		return err
	}
	werr := tools.WriteExecutable(dst, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
	return errors.Join(werr, in.Close())
}

// Version returns the version the rooket binary bin reports: the second word
// of the first line bin version prints, v0.1.0 from "rooket v0.1.0".
func Version(ctx context.Context, r Runner, bin string) (string, error) {
	out, err := r.Output(ctx, "", nil, bin, "version")
	if err != nil {
		return "", fmt.Errorf("reading rooket's version: %w", err)
	}
	first, _, _ := strings.Cut(string(out), "\n")
	words := strings.Fields(first)
	if len(words) < 2 {
		return "", fmt.Errorf("reading rooket's version: no version in the first line of %q", out)
	}
	return words[1], nil
}
