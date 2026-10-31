package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/jhoblitt/rooket-action/internal/cli"
	"github.com/jhoblitt/rooket-action/internal/cli/clifakes"
	"github.com/jhoblitt/rooket-action/internal/tools"
)

// helper is where the action's main step builds rooket-action.
const helper = "/home/runner/work/_temp/rooket-action/bin/rooket-action"

const gib = uint64(1) << 30

// hosted is the environment of a GitHub-hosted Linux X64 runner.
func hosted() map[string]string {
	return map[string]string{
		"GITHUB_ACTIONS":     "true",
		"RUNNER_ENVIRONMENT": "github-hosted",
		"RUNNER_OS":          "Linux",
		"RUNNER_ARCH":        "X64",
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func expectContent(path, want string) {
	GinkgoHelper()
	Expect(os.ReadFile(path)).To(BeEquivalentTo(want), "content of %s", path)
}

func expectInstalled(path string, want []byte) {
	GinkgoHelper()
	Expect(os.ReadFile(path)).To(Equal(want), "content of %s", path)
	info, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	Expect(info.Mode().Perm()).To(Equal(fs.FileMode(0o755)), "mode of %s", path)
}

var _ = Describe("setup", func() {
	var (
		runner                                   *clifakes.FakeRunner
		env                                      map[string]string
		binDir, src                              string
		outputFile, stateFile, pathFile, envFile string
		bin, kind                                []byte
		free                                     uint64
		hits                                     *atomic.Int32
		stdout, stderr                           *bytes.Buffer
		d                                        cli.Deps
		args                                     []string
	)

	setup := func(ctx context.Context, args ...string) error {
		return cli.RunWith(ctx, append([]string{"setup"}, args...), stdout, stderr,
			func(io.Writer, io.Writer) cli.Deps { return d })
	}

	// expectUntouched asserts that setup stopped before changing anything.
	expectUntouched := func() {
		GinkgoHelper()
		Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
		Expect(runner.OutputCallCount()).To(BeZero(), "Output calls")
		Expect(hits.Load()).To(BeZero(), "HTTP requests")
		Expect(binDir).NotTo(BeADirectory())
		for _, f := range []string{outputFile, stateFile, pathFile, envFile} {
			expectContent(f, "")
		}
	}

	BeforeEach(func() {
		prev := slog.Default()
		DeferCleanup(slog.SetDefault, prev)

		dir := GinkgoT().TempDir()
		binDir = filepath.Join(dir, "rooket-action", "bin")
		outputFile = filepath.Join(dir, "set_output")
		stateFile = filepath.Join(dir, "save_state")
		pathFile = filepath.Join(dir, "add_path")
		envFile = filepath.Join(dir, "set_env")
		for _, f := range []string{outputFile, stateFile, pathFile, envFile} {
			Expect(os.WriteFile(f, nil, 0o600)).To(Succeed())
		}
		bin = []byte("rooket binary")
		src = filepath.Join(dir, "artifact", "rooket")
		Expect(os.MkdirAll(filepath.Dir(src), 0o755)).To(Succeed())
		Expect(os.WriteFile(src, bin, 0o644)).To(Succeed())

		env = hosted()
		env["GITHUB_OUTPUT"] = outputFile
		env["GITHUB_STATE"] = stateFile
		env["GITHUB_PATH"] = pathFile
		env["GITHUB_ENV"] = envFile

		runner = new(clifakes.FakeRunner)
		runner.OutputReturns([]byte("rooket v0.1.0\n"), nil)
		free = 1 << 50
		hits = new(atomic.Int32)
		stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)

		kind = []byte("kind binary")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if r.URL.Path != "/kind" {
				http.NotFound(w, r)
				return
			}
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(kind))
		}))
		DeferCleanup(srv.Close)

		d = cli.Deps{
			Runner: runner,
			Getenv: func(key string) string { return env[key] },
			Free:   func(string) (uint64, error) { return free, nil },
			HTTP:   srv.Client(),
			Retry:  tools.Retry{Attempts: 1, PerRequest: 5 * time.Second, Backoff: time.Millisecond},
			Pins: []tools.Pin{
				{Name: "kind", Version: "v0.32.0", URL: srv.URL + "/kind", SHA256: sha(kind)},
			},
			Executable: func() (string, error) { return helper, nil },
		}
		args = []string{"--bin-dir", binDir, "--rooket-path", src}
	})

	It("installs rooket and the pins, prepares the runner, and writes the outputs and the state", func(ctx SpecContext) {
		Expect(setup(ctx, args...)).To(Succeed())

		rooketPath := filepath.Join(binDir, "rooket")
		expectInstalled(rooketPath, bin)
		expectInstalled(filepath.Join(binDir, "kind"), kind)
		expectContent(outputFile, "path="+rooketPath+"\nrooket-version=v0.1.0\n")
		// helper last: the post step keys on it, so it marks a setup that
		// recorded everything.
		expectContent(stateFile, "rooket="+rooketPath+"\nhelper="+helper+"\n")
		expectContent(pathFile, binDir+"\n")
		expectContent(envFile, "ROOKET_ENGINE=docker\n")

		Expect(runner.OutputCallCount()).To(Equal(1), "Output calls")
		_, _, _, name, versionArgs := runner.OutputArgsForCall(0)
		Expect(name).To(Equal(rooketPath))
		Expect(versionArgs).To(Equal([]string{"version"}))
	})

	It("sets up iSCSI by default, with its log group on the command's stdout", func(ctx SpecContext) {
		Expect(setup(ctx, args...)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(4), "Run calls: SetupISCSI's commands")
		Expect(stdout.String()).To(Equal("::group::Installing iSCSI and LVM tooling\n::endgroup::\n"))
	})

	It("with --iscsi FALSE, runs no iSCSI command", func(ctx SpecContext) {
		Expect(setup(ctx, append(args, "--iscsi", "FALSE")...)).To(Succeed())

		Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
		Expect(stdout.String()).To(BeEmpty())
	})

	DescribeTable("sizes the disk check by --workers and --min-free-gb",
		func(ctx SpecContext, needGiB string, extra ...string) {
			free = 20 * gib

			Expect(setup(ctx, append(args, append(extra, "--iscsi", "false")...)...)).To(Succeed())

			Expect(stdout.String()).To(Equal(
				"::warning::20.0 GiB free on / after deleting preinstalled toolchains; " + needGiB + " GiB needed\n"))
		},
		Entry("one worker by default", "40.0"),
		Entry("three workers", "60.0", "--workers", "3"),
		Entry("min-free-gb overriding the formula", "25.0", "--workers", "3", "--min-free-gb", "25"),
	)

	DescribeTable("accepts --teardown true or false in any case, and records the state either way",
		func(ctx SpecContext, value string) {
			Expect(setup(ctx, append(args, "--teardown", value)...)).To(Succeed())

			rooketPath := filepath.Join(binDir, "rooket")
			expectContent(stateFile, "rooket="+rooketPath+"\nhelper="+helper+"\n")
		},
		Entry("TRUE", "TRUE"),
		Entry("False", "False"),
	)

	It("runs go install with --go and --go-mod-cache when --rooket-version is set alone", func(ctx SpecContext) {
		const (
			goBin      = "/opt/hostedtoolcache/go/1.27.0/x64/bin/go"
			goModCache = "/home/runner/work/_temp/rooket-action/gomodcache"
		)

		Expect(setup(ctx, "--bin-dir", binDir, "--go", goBin, "--go-mod-cache", goModCache,
			"--rooket-version", " main ", "--iscsi", "false")).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(1), "Run calls")
		_, goEnv, name, goArgs := runner.RunArgsForCall(0)
		Expect(name).To(Equal(goBin))
		Expect(goArgs).To(Equal([]string{"install", "github.com/jhoblitt/rooket@main"}))
		Expect(goEnv).To(ContainElements("GOBIN="+binDir, "GOMODCACHE="+goModCache, "GOTOOLCHAIN=auto"))
		Expect(binDir).To(BeADirectory())
	})

	It("replaces rooket and the pins in place on a second setup into the same --bin-dir", func(ctx SpecContext) {
		Expect(setup(ctx, args...)).To(Succeed())
		second := []byte("second rooket binary")
		Expect(os.WriteFile(src, second, 0o644)).To(Succeed())

		Expect(setup(ctx, args...)).To(Succeed())

		expectInstalled(filepath.Join(binDir, "rooket"), second)
		expectInstalled(filepath.Join(binDir, "kind"), kind)
	})

	It("builds its dependencies around the command's own output streams", func(ctx SpecContext) {
		var gotOut, gotErr io.Writer

		Expect(cli.RunWith(ctx, append([]string{"setup"}, args...), stdout, stderr,
			func(o, e io.Writer) cli.Deps {
				gotOut, gotErr = o, e
				return d
			})).To(Succeed())

		Expect(gotOut).To(BeIdenticalTo(stdout))
		Expect(gotErr).To(BeIdenticalTo(stderr))
	})

	DescribeTable("writes no state when a step before it fails, so the post step tears nothing down",
		func(ctx SpecContext, fail func() types.GomegaMatcher) {
			cause := fail()

			Expect(setup(ctx, args...)).To(cause)

			expectContent(stateFile, "")
			expectContent(outputFile, "")
		},
		// With --rooket-path, prepare's iSCSI step makes the only Run calls.
		Entry("prepare failing", func() types.GomegaMatcher {
			err := errors.New("exit status 100")
			runner.RunReturns(err)
			return And(MatchError(err), MatchError(ContainSubstring("updating the apt package lists")))
		}),
		// rooket version makes the only Output call.
		Entry("rooket version failing", func() types.GomegaMatcher {
			err := errors.New("exit status 2")
			runner.OutputReturns(nil, err)
			return MatchError(err)
		}),
		Entry("the outputs failing to write", func() types.GomegaMatcher {
			env["GITHUB_OUTPUT"] = GinkgoT().TempDir()
			return And(MatchError(ContainSubstring("GITHUB_OUTPUT")), MatchError(syscall.EISDIR))
		}),
	)

	It("refuses a non-hosted runner first, before checking anything else or touching anything", func(ctx SpecContext) {
		env["RUNNER_ENVIRONMENT"] = "self-hosted"
		delete(env, "GITHUB_OUTPUT")

		err := setup(ctx, append(args, "--workers", "three")...)

		Expect(err).To(MatchError(And(
			ContainSubstring("RUNNER_ENVIRONMENT"),
			Not(ContainSubstring("GITHUB_OUTPUT")),
			Not(ContainSubstring("workers")),
		)))
		expectUntouched()
	})

	DescribeTable("requires each file it writes through, before touching anything",
		func(ctx SpecContext, name string) {
			delete(env, name)

			Expect(setup(ctx, args...)).To(MatchError(ContainSubstring(name)))

			expectUntouched()
		},
		Entry("GITHUB_OUTPUT unset", "GITHUB_OUTPUT"),
		Entry("GITHUB_STATE unset", "GITHUB_STATE"),
	)

	DescribeTable("refuses invalid inputs before touching anything, naming each",
		func(ctx SpecContext, extra []string, want ...string) {
			matchers := make([]types.GomegaMatcher, 0, len(want))
			for _, w := range want {
				matchers = append(matchers, ContainSubstring(w))
			}

			Expect(setup(ctx, append(args, extra...)...)).To(MatchError(And(matchers...)))

			expectUntouched()
		},
		Entry("iscsi not a boolean", []string{"--iscsi", "no"}, `input iscsi: "no" is not true or false`),
		Entry("workers not a number", []string{"--workers", "three"}, `input workers: "three"`),
		Entry("workers below 1", []string{"--workers", "0"}, `input workers: "0"`),
		Entry("min-free-gb negative", []string{"--min-free-gb", "-5"}, `input min-free-gb: "-5"`),
		// Only the post step acts on teardown; setup checks it so that a typo
		// fails the main step rather than the post step.
		Entry("teardown not a boolean", []string{"--teardown", "no"}, `input teardown: "no" is not true or false`),
		Entry("rooket-path with rooket-version", []string{"--rooket-version", "v1.2.3"}, "input rooket-path", "rooket-version"),
		Entry("two at once", []string{"--iscsi", "no", "--workers", "0"}, "input iscsi", "input workers"),
	)

	It("refuses a relative --bin-dir before touching anything, because GITHUB_PATH needs an absolute one", func(ctx SpecContext) {
		GinkgoT().Chdir(GinkgoT().TempDir())
		rel := filepath.Join("rooket-action", "bin")

		Expect(setup(ctx, "--bin-dir", rel, "--rooket-path", src)).To(MatchError(ContainSubstring("--bin-dir %q", rel)))

		expectUntouched()
		Expect(rel).NotTo(BeADirectory())
	})

	It("stops before touching anything when the helper cannot find its own path", func(ctx SpecContext) {
		d.Executable = func() (string, error) { return "", errors.New("readlink /proc/self/exe: no such file") }

		Expect(setup(ctx, args...)).To(MatchError(ContainSubstring("/proc/self/exe")))

		expectUntouched()
	})

	DescribeTable("checks its required flags before touching anything",
		func(ctx SpecContext, want string, flags ...string) {
			Expect(setup(ctx, flags...)).To(MatchError(ContainSubstring(want)))

			expectUntouched()
		},
		Entry("--bin-dir missing", "--bin-dir is required", "--rooket-path", "/rooket"),
		Entry("--go missing when go install would run", "--go is required",
			"--bin-dir", "/rooket-action/bin", "--go-mod-cache", "/rooket-action/gomodcache", "--rooket-version", "main"),
		Entry("--go-mod-cache missing when go install would run", "--go-mod-cache is required",
			"--bin-dir", "/rooket-action/bin", "--go", "/go", "--rooket-version", "main"),
	)
})
