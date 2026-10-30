// Package sys runs commands on the host and measures its disks.
package sys

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Exec runs host commands. In both of its methods, env holds KEY=VALUE
// entries layered over the parent's environment, so an entry overrides the
// parent's value for the same key, and an error names the command.
type Exec struct {
	// Stdout receives Run's child stdout.
	Stdout io.Writer
	// Stderr receives the child's stderr from Run and Output alike.
	Stderr io.Writer
}

// Run runs name with args, streaming the child's stdout and stderr to
// e.Stdout and e.Stderr.
func (e Exec) Run(ctx context.Context, env []string, name string, args ...string) error {
	cmd := e.command(ctx, "", env, name, args)
	cmd.Stdout = e.Stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running %s: %w", commandLine(name, args), err)
	}
	return nil
}

// Output runs name with args in dir and returns the child's stdout, passing
// its stderr to e.Stderr. An empty dir is the parent's working directory.
func (e Exec) Output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	out, err := e.command(ctx, dir, env, name, args).Output()
	if err != nil {
		return nil, fmt.Errorf("running %s: %w", commandLine(name, args), err)
	}
	return out, nil
}

func (e Exec) command(ctx context.Context, dir string, env []string, name string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // running the command its caller names is Exec's job; no shell parses name or args
	cmd.Dir = dir
	// Environ, called after Dir is set, points PWD at dir; exec keeps the
	// last of duplicate keys, which is what lets env override the parent.
	cmd.Env = append(cmd.Environ(), env...)
	cmd.Stderr = e.Stderr
	return cmd
}

func commandLine(name string, args []string) string {
	return strings.Join(append([]string{name}, args...), " ")
}
