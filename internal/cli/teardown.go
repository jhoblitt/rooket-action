package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jhoblitt/rooket-action/internal/ghcmd"
	"github.com/jhoblitt/rooket-action/internal/prepare"
)

// newTeardownCmd returns the teardown subcommand, which the action's post
// step runs once setup has succeeded.
func newTeardownCmd(v *viper.Viper, nd newDeps) *cobra.Command {
	var enabled bool
	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Tear down every rooket cluster on this runner",
		Long: `Tear down every rooket cluster on this runner with
rooket down --all --force --delete-disks. A teardown that fails, that has
no rooket binary to run, or that setup's guard refuses on this machine,
prints a ::warning:: workflow command and still exits 0: by the time the
post step runs, the job's result is decided, and the runner is discarded
after it. Only an invalid --teardown, or another flag error, exits non-zero.`,
		Args: cobra.NoArgs,
		PreRunE: func(*cobra.Command, []string) error {
			var err error
			enabled, err = parseBool("teardown", v.GetString("teardown"))
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !enabled {
				slog.InfoContext(cmd.Context(), "teardown disabled")
				return nil
			}
			d := nd(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err := prepare.Guard(d.Getenv); err != nil {
				ghcmd.Warning(cmd.OutOrStdout(), fmt.Sprintf("not tearing down rooket clusters: %v", err))
				return nil
			}
			bin := v.GetString("rooket")
			if bin == "" {
				ghcmd.Warning(cmd.OutOrStdout(), "not tearing down rooket clusters: --rooket names no rooket binary")
				return nil
			}
			teardown(cmd.Context(), d.Runner, bin, cmd.OutOrStdout())
			return nil
		},
	}
	f := cmd.Flags()
	f.String("rooket", "", "the rooket binary setup installed")
	f.String("teardown", "true", "the teardown input: true or false, in any case")
	return cmd
}

// teardown runs bin down for every rooket cluster, and reports a failure as
// a ::warning:: on out.
func teardown(ctx context.Context, r Runner, bin string, out io.Writer) {
	slog.InfoContext(ctx, "tearing down rooket clusters", slog.String("rooket", bin))
	err := r.Run(ctx, []string{"ROOKET_ENGINE=docker"}, bin, "down", "--all", "--force", "--delete-disks")
	if err != nil {
		ghcmd.Warning(out, fmt.Sprintf("tearing down rooket clusters: %v", err))
	}
}
