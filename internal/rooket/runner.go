package rooket

import "context"

//go:generate go tool counterfeiter -generate

// Runner runs host commands; sys.Exec is the one production uses. env holds
// KEY=VALUE entries layered over the parent's environment.
//
//counterfeiter:generate . Runner
type Runner interface {
	// Run runs name with args, streaming the child's output.
	Run(ctx context.Context, env []string, name string, args ...string) error
	// Output runs name with args in dir and returns the child's stdout.
	Output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)
}
