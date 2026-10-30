package cli

import (
	"context"
	"io"
	"strings"
)

// Deps are the subcommands' dependencies, which a spec fills with fakes.
type Deps = deps

// ProductionDeps is what Run builds each subcommand's Deps with.
var ProductionDeps = productionDeps

// The input parsers.
var (
	ParseBool          = parseBool
	ParseCount         = parseCount
	ParseOptionalCount = parseOptionalCount
)

// RunWith runs the command tree as Run does, except that nd builds each
// subcommand's Deps from the command's stdout and stderr.
func RunWith(ctx context.Context, args []string, stdout, stderr io.Writer, nd func(stdout, stderr io.Writer) Deps) error {
	return run(ctx, args, strings.NewReader(""), stdout, stderr, nd)
}
