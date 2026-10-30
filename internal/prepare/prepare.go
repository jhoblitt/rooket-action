// Package prepare readies a disposable GitHub-hosted runner for rooket.
package prepare

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jhoblitt/rooket-action/internal/ghfiles"
	"github.com/jhoblitt/rooket-action/internal/tools"
)

// Config is what the action's inputs decide about the runner.
type Config struct {
	// Workers is rooket's worker count, which sizes the disk step.
	Workers int
	// MinFreeGiB, when above 0, replaces the disk step's formula.
	MinFreeGiB int
	// ISCSI enables the iSCSI and LVM step.
	ISCSI bool
	// BinDir is the absolute directory the pins are installed into and
	// GITHUB_PATH gains. Run creates it.
	BinDir string
}

// Deps are the effects Run has outside the process.
type Deps struct {
	// Runner runs the disk and iSCSI steps' commands.
	Runner Runner
	// Getenv reads the environment, as os.Getenv does.
	Getenv func(key string) string
	// Free returns the bytes available on the filesystem holding path, as
	// sys.Free does.
	Free func(path string) (uint64, error)
	// HTTP downloads the pins, retrying as Retry allows.
	HTTP  *http.Client
	Retry tools.Retry
	// Pins are the tools Run installs into BinDir; production's are
	// tools.Pins().
	Pins []tools.Pin
	// Out receives GitHub workflow commands: the disk step's warnings, and
	// the log group the iSCSI step's output folds into.
	Out io.Writer
}

// Run prepares the runner, stopping at the first step that fails:
//
//  1. Guard refuses any machine but a GitHub-hosted Linux X64 runner.
//  2. GITHUB_PATH and GITHUB_ENV must be set and BinDir absolute, so that a
//     run unable to export its result fails before it changes anything.
//  3. BinDir is created.
//  4. EnsureDisk frees disk space, which fails only once ctx is done.
//  5. Each pin is installed into BinDir.
//  6. When cfg.ISCSI is set, SetupISCSI runs inside a log group on d.Out.
//  7. BinDir is appended to GITHUB_PATH, then ROOKET_ENGINE=docker to
//     GITHUB_ENV, so a failure anywhere earlier exports nothing.
func Run(ctx context.Context, cfg Config, d Deps) error {
	if err := Guard(d.Getenv); err != nil {
		return err
	}
	githubPath, err := commandFile(d.Getenv, "GITHUB_PATH")
	if err != nil {
		return err
	}
	githubEnv, err := commandFile(d.Getenv, "GITHUB_ENV")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(cfg.BinDir) {
		return fmt.Errorf("binary directory %q is not absolute, and GITHUB_PATH needs an absolute one", cfg.BinDir)
	}

	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil { //nolint:gosec // G301: BinDir goes on PATH, and the binaries in it are 0755 for any user to run
		return fmt.Errorf("creating the binary directory: %w", err)
	}
	if err := EnsureDisk(ctx, d.Runner, d.Free, Threshold(cfg.Workers, cfg.MinFreeGiB), d.Out); err != nil {
		return err
	}
	for _, p := range d.Pins {
		slog.InfoContext(ctx, "installing pinned tool",
			slog.String("tool", p.Name),
			slog.String("version", p.Version))
		if err := tools.Install(ctx, d.HTTP, p, cfg.BinDir, d.Retry); err != nil {
			return err
		}
	}
	if cfg.ISCSI {
		fmt.Fprintln(d.Out, "::group::Installing iSCSI and LVM tooling")
		err := SetupISCSI(ctx, d.Runner)
		// Closed before the error is reported, which would otherwise be
		// folded away inside the group.
		fmt.Fprintln(d.Out, "::endgroup::")
		if err != nil {
			return err
		}
	}

	if err := ghfiles.AddPath(githubPath, cfg.BinDir); err != nil {
		return fmt.Errorf("adding the binary directory to GITHUB_PATH: %w", err)
	}
	if err := ghfiles.Set(githubEnv, "ROOKET_ENGINE", "docker"); err != nil {
		return fmt.Errorf("setting ROOKET_ENGINE in GITHUB_ENV: %w", err)
	}
	return nil
}

// commandFile returns the path of the runner command file the environment
// variable name holds, or an error naming name when it is unset or empty.
func commandFile(getenv func(string) string, name string) (string, error) {
	if path := getenv(name); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("environment variable %s is not set; prepare exports through the file it names", name)
}
