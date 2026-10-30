package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jhoblitt/rooket-action/internal/ghfiles"
	"github.com/jhoblitt/rooket-action/internal/prepare"
	"github.com/jhoblitt/rooket-action/internal/rooket"
)

// newSetupCmd returns the setup subcommand, which the action's main step
// runs.
func newSetupCmd(v *viper.Viper, nd newDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install rooket and prepare this runner to run it",
		Long: `Install rooket into --bin-dir and prepare this GitHub-hosted runner to run
it: free disk space, install the pinned kind, kubectl and helm, set up iSCSI,
and export --bin-dir and ROOKET_ENGINE=docker to the job's later steps. Then
write the path and rooket-version outputs to GITHUB_OUTPUT and, last, what
the post step's teardown needs to GITHUB_STATE.

The flags named for the action's inputs take each input's value as given.
--teardown is only checked here, so that an invalid value fails this step
rather than the post step, which acts on it.`,
		Args: cobra.NoArgs,
		PreRunE: func(*cobra.Command, []string) error {
			if v.GetString("bin-dir") == "" {
				return errors.New("--bin-dir is required")
			}
			if rooketOptions(v).UsesGo() && v.GetString("go") == "" {
				return errors.New("--go is required when --rooket-version is set without --rooket-sha256, which installs rooket with go install")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setup(cmd.Context(), v, nd(cmd.OutOrStdout(), cmd.ErrOrStderr()), cmd.OutOrStdout())
		},
	}
	f := cmd.Flags()
	f.String("go", "", "the go binary, needed when --rooket-version is set without --rooket-sha256")
	f.String("bin-dir", "", "the absolute directory to install into and add to GITHUB_PATH")
	f.String("rooket-version", "", "the rooket-version input")
	f.String("rooket-sha256", "", "the rooket-sha256 input")
	f.String("rooket-path", "", "the rooket-path input")
	f.String("workers", "1", "the workers input")
	f.String("min-free-gb", "", "the min-free-gb input")
	f.String("iscsi", "true", "the iscsi input: true or false, in any case")
	f.String("teardown", "true", "the teardown input: true or false, in any case; only checked here")
	return cmd
}

func rooketOptions(v *viper.Viper) rooket.Options {
	return rooket.Options{
		Go:      v.GetString("go"),
		BinDir:  v.GetString("bin-dir"),
		Version: v.GetString("rooket-version"),
		SHA256:  v.GetString("rooket-sha256"),
		Path:    v.GetString("rooket-path"),
	}
}

// setupInputs are setup's flags, parsed and checked.
type setupInputs struct {
	prepare prepare.Config
	rooket  rooket.Options
}

// readSetupInputs parses and checks every setup flag, and reports every
// invalid one at once.
func readSetupInputs(v *viper.Viper) (setupInputs, error) {
	iscsi, iscsiErr := parseBool("iscsi", v.GetString("iscsi"))
	_, teardownErr := parseBool("teardown", v.GetString("teardown"))
	workers, workersErr := parseCount("workers", v.GetString("workers"))
	minFree, minFreeErr := parseOptionalCount("min-free-gb", v.GetString("min-free-gb"))
	opts := rooketOptions(v)
	var binDirErr error
	if !filepath.IsAbs(opts.BinDir) {
		binDirErr = fmt.Errorf("--bin-dir %q is not absolute, and GITHUB_PATH needs an absolute directory", opts.BinDir)
	}
	if err := errors.Join(iscsiErr, teardownErr, workersErr, minFreeErr, opts.Validate(), binDirErr); err != nil {
		return setupInputs{}, err
	}
	return setupInputs{
		prepare: prepare.Config{Workers: workers, MinFreeGiB: minFree, ISCSI: iscsi, BinDir: opts.BinDir},
		rooket:  opts,
	}, nil
}

// setup installs rooket, prepares the runner, and records the result,
// stopping at the first step that fails:
//
//  1. prepare.Guard refuses any machine but a GitHub-hosted Linux X64
//     runner, before anything else is checked.
//  2. GITHUB_OUTPUT and GITHUB_STATE must be set, every flag valid, and the
//     helper's own path known, so that a run unable to record its result
//     fails before it changes anything.
//  3. BinDir is created, and rooket installed into it.
//  4. prepare.Run prepares the runner.
//  5. rooket's path and the version it reports go to GITHUB_OUTPUT.
//  6. rooket's path, then the helper's, go to GITHUB_STATE, last. The post
//     step runs only when helper is there, so helper marks a setup that
//     recorded everything: there is no teardown after a failed setup, nor
//     on a runner the guard refused.
func setup(ctx context.Context, v *viper.Viper, d deps, out io.Writer) error {
	if err := prepare.Guard(d.Getenv); err != nil {
		return err
	}
	outputFile, err := commandFile(d.Getenv, "GITHUB_OUTPUT")
	if err != nil {
		return err
	}
	stateFile, err := commandFile(d.Getenv, "GITHUB_STATE")
	if err != nil {
		return err
	}
	in, err := readSetupInputs(v)
	if err != nil {
		return err
	}
	helper, err := d.Executable()
	if err != nil {
		return fmt.Errorf("locating the helper binary: %w", err)
	}

	err = os.MkdirAll(in.prepare.BinDir, 0o755) //nolint:gosec // G301: BinDir goes on PATH, and the binaries in it are 0755 for any user to run
	if err != nil {
		return fmt.Errorf("creating the binary directory: %w", err)
	}
	path, err := rooket.Install(ctx, d.Runner, d.HTTP, d.Retry, in.rooket)
	if err != nil {
		return err
	}
	err = prepare.Run(ctx, in.prepare, prepare.Deps{
		Runner: d.Runner,
		Getenv: d.Getenv,
		Free:   d.Free,
		HTTP:   d.HTTP,
		Retry:  d.Retry,
		Pins:   d.Pins,
		Out:    out,
	})
	if err != nil {
		return err
	}
	version, err := rooket.Version(ctx, d.Runner, path)
	if err != nil {
		return err
	}

	for _, rec := range []struct{ file, fileVar, key, value string }{
		{outputFile, "GITHUB_OUTPUT", "path", path},
		{outputFile, "GITHUB_OUTPUT", "rooket-version", version},
		{stateFile, "GITHUB_STATE", "rooket", path},
		{stateFile, "GITHUB_STATE", "helper", helper},
	} {
		if err := ghfiles.Set(rec.file, rec.key, rec.value); err != nil {
			return fmt.Errorf("setting %s in %s: %w", rec.key, rec.fileVar, err)
		}
	}
	return nil
}

// commandFile returns the path of the runner command file the environment
// variable name holds, or an error naming name when it is unset or empty.
func commandFile(getenv func(string) string, name string) (string, error) {
	if path := getenv(name); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("environment variable %s is not set; setup records its result through the file it names", name)
}
