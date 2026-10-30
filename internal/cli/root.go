// Package cli builds the rooket-action command tree.
//
// Managed by go-conventions (references/cli.md owns the cobra and viper
// contract, references/logging.md the logger). Configuration precedence is
// flags, then ROOKET_ACTION_* environment variables, then the config file,
// then defaults, and every value is read back through viper.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jhoblitt/rooket-action/internal/version"
)

// Run executes the command tree against args and returns the first error.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return run(ctx, args, stdin, stdout, stderr, productionDeps)
}

// run is Run with the subcommands' dependencies built by nd.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, nd newDeps) error {
	cmd := newRootCmd(stdin, stdout, stderr, nd)
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func newRootCmd(stdin io.Reader, stdout, stderr io.Writer, nd newDeps) *cobra.Command {
	v := viper.New()
	cmd := &cobra.Command{
		Use:           "rooket-action",
		Short:         "Prepares a GitHub-hosted Ubuntu runner to run rooket, and tears down what rooket left behind.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return configure(cmd, v, stderr)
		},
	}
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	pf := cmd.PersistentFlags()
	pf.String("config", "", "config file to read after flags and environment")
	pf.String("log-level", "info", "log level: debug, info, warn, or error (ROOKET_ACTION_LOG_LEVEL)")
	pf.String("log-format", "json", "log format: json or text (ROOKET_ACTION_LOG_FORMAT)")

	cmd.AddCommand(newSetupCmd(v, nd), newTeardownCmd(v, nd))
	return cmd
}

// configure binds the command's flags and the environment into v, reads the
// config file when one is named, and installs the default logger. It runs
// before every command, so a subcommand's own flags are bound as well.
func configure(cmd *cobra.Command, v *viper.Viper, stderr io.Writer) error {
	v.SetEnvPrefix("ROOKET_ACTION")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("binding flags: %w", err)
	}
	if cfg := v.GetString("config"); cfg != "" {
		v.SetConfigFile(cfg)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config %s: %w", cfg, err)
		}
	}
	return setupLogging(v, stderr)
}

func setupLogging(v *viper.Viper, w io.Writer) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(v.GetString("log-level"))); err != nil {
		return fmt.Errorf("parsing log level: %w", err)
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch format := v.GetString("log-format"); format {
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		return fmt.Errorf("unknown log format %q", format)
	}
	slog.SetDefault(slog.New(handler))
	return nil
}
