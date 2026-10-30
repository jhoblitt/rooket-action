package cli

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/jhoblitt/rooket-action/internal/sys"
	"github.com/jhoblitt/rooket-action/internal/tools"
)

//go:generate go tool counterfeiter -generate

// Runner runs host commands for setup and teardown; sys.Exec is the one
// production uses. It is the prepare.Runner and the rooket.Runner setup
// hands on.
//
//counterfeiter:generate . Runner
type Runner interface {
	// Run runs name with args, streaming the child's output.
	Run(ctx context.Context, env []string, name string, args ...string) error
	// Output runs name with args in dir and returns the child's stdout.
	Output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)
}

// deps are the effects setup and teardown have outside the process.
type deps struct {
	// Runner runs host commands.
	Runner Runner
	// Getenv reads the environment, as os.Getenv does.
	Getenv func(key string) string
	// Free returns the bytes available on the filesystem holding path, as
	// sys.Free does.
	Free func(path string) (uint64, error)
	// HTTP downloads rooket and the pins, retrying as Retry allows.
	HTTP  *http.Client
	Retry tools.Retry
	// Pins are the tools prepare installs beside rooket.
	Pins []tools.Pin
	// Executable returns the path of the running helper, as os.Executable
	// does.
	Executable func() (string, error)
}

// newDeps builds a command's deps around its output streams.
type newDeps func(stdout, stderr io.Writer) deps

// productionDeps returns the deps a command acts on the runner through, with
// its commands' output streamed to stdout and stderr. A command passes its
// own cmd.OutOrStdout() and cmd.ErrOrStderr(), so that workflow commands and
// child output share one stream.
func productionDeps(stdout, stderr io.Writer) deps {
	return deps{
		Runner:     sys.Exec{Stdout: stdout, Stderr: stderr},
		Getenv:     os.Getenv,
		Free:       sys.Free,
		HTTP:       http.DefaultClient,
		Retry:      tools.DefaultRetry,
		Pins:       tools.Pins(),
		Executable: os.Executable,
	}
}
