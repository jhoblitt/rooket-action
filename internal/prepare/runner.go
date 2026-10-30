// Package prepare readies a disposable GitHub-hosted runner for rooket.
package prepare

import "context"

//go:generate go tool counterfeiter -generate

// Runner runs a host command, streaming its output; sys.Exec is the one
// production uses. env holds KEY=VALUE entries layered over the parent's
// environment.
//
//counterfeiter:generate . Runner
type Runner interface {
	Run(ctx context.Context, env []string, name string, args ...string) error
}
