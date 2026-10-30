package prepare_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/prepare"
	"github.com/jhoblitt/rooket-action/internal/prepare/preparefakes"
	"github.com/jhoblitt/rooket-action/internal/tools"
)

// What the runner's GITHUB_PATH and GITHUB_ENV files hold before Run, so a
// spec can tell an append from an overwrite and an untouched file from both.
const (
	pathSeed = "/opt/earlier-step/bin\n"
	envSeed  = "EARLIER_STEP=1\n"
)

// recorder logs, in order, what Run's dependencies see. It is also an
// io.Writer that logs each line written to it.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func (r *recorder) Write(p []byte) (int, error) {
	for line := range strings.Lines(string(p)) {
		r.add(strings.TrimSuffix(line, "\n"))
	}
	return len(p), nil
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func read(path string) string {
	GinkgoHelper()
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}

func expectContent(path, want string) {
	GinkgoHelper()
	Expect(read(path)).To(Equal(want), "content of %s", path)
}

var _ = Describe("Run", func() {
	var (
		runner            *preparefakes.FakeRunner
		rec               *recorder
		hits              *atomic.Int32
		env               map[string]string
		pathFile, envFile string
		binDir            string
		free              uint64
		out               *bytes.Buffer
		bins              map[string][]byte
		cfg               prepare.Config
		deps              prepare.Deps
	)

	// expectUntouched asserts that Run stopped before changing the machine.
	expectUntouched := func() {
		GinkgoHelper()
		Expect(runner.RunCallCount()).To(Equal(0), "runner calls")
		Expect(hits.Load()).To(Equal(int32(0)), "HTTP requests")
		Expect(rec.all()).To(BeEmpty(), "what Run's dependencies saw")
		Expect(binDir).NotTo(BeADirectory())
		expectContent(pathFile, pathSeed)
		expectContent(envFile, envSeed)
	}

	BeforeEach(func() {
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(GinkgoWriter, nil)))
		DeferCleanup(slog.SetDefault, prev)

		dir := GinkgoT().TempDir()
		pathFile = filepath.Join(dir, "add_path")
		envFile = filepath.Join(dir, "set_env")
		Expect(os.WriteFile(pathFile, []byte(pathSeed), 0o600)).To(Succeed())
		Expect(os.WriteFile(envFile, []byte(envSeed), 0o600)).To(Succeed())
		binDir = filepath.Join(dir, "rooket-action", "bin")

		env = hosted()
		env["GITHUB_PATH"] = pathFile
		env["GITHUB_ENV"] = envFile

		runner = new(preparefakes.FakeRunner)
		rec = new(recorder)
		hits = new(atomic.Int32)
		free = 1 << 50
		out = new(bytes.Buffer)

		bins = map[string][]byte{
			"/kind":    []byte("kind binary"),
			"/kubectl": []byte("kubectl binary"),
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			rec.add(r.Method + " " + r.URL.Path)
			b, ok := bins[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
		}))
		DeferCleanup(srv.Close)

		cfg = prepare.Config{Workers: 1, ISCSI: true, BinDir: binDir}
		deps = prepare.Deps{
			Runner: runner,
			Getenv: getenv(env),
			Free: func(path string) (uint64, error) {
				rec.add("free " + path)
				return free, nil
			},
			HTTP:  srv.Client(),
			Retry: tools.Retry{Attempts: 1, PerRequest: 5 * time.Second, Backoff: time.Millisecond},
			Pins: []tools.Pin{
				{Name: "kind", URL: srv.URL + "/kind", SHA256: sha(bins["/kind"])},
				{Name: "kubectl", URL: srv.URL + "/kubectl", SHA256: sha(bins["/kubectl"])},
			},
			Out: out,
		}
	})

	It("measures the disk, downloads each pin, then sets up iSCSI inside a log group", func(ctx SpecContext) {
		runner.RunCalls(func(_ context.Context, _ []string, name string, args ...string) error {
			rec.add(strings.Join(append([]string{name}, args...), " "))
			return nil
		})
		deps.Out = rec

		Expect(prepare.Run(ctx, cfg, deps)).To(Succeed())

		Expect(rec.all()).To(Equal([]string{
			"free /",
			"GET /kind",
			"GET /kubectl",
			"::group::Installing iSCSI and LVM tooling",
			"sudo apt-get update",
			"sudo apt-get install -y open-iscsi targetcli-fb lvm2",
			"sudo modprobe -a configfs target_core_mod target_core_file iscsi_target_mod iscsi_tcp",
			"sudo systemctl enable --now iscsid",
			"::endgroup::",
		}))
	})

	It("installs each pin into BinDir, creating the directory", func(ctx SpecContext) {
		Expect(prepare.Run(ctx, cfg, deps)).To(Succeed())

		for name, want := range map[string][]byte{"kind": bins["/kind"], "kubectl": bins["/kubectl"]} {
			Expect(os.ReadFile(filepath.Join(binDir, name))).To(Equal(want), "content of %s", name)
		}
		Expect(hits.Load()).To(Equal(int32(2)), "HTTP requests")
	})

	It("appends BinDir to GITHUB_PATH and ROOKET_ENGINE=docker to GITHUB_ENV", func(ctx SpecContext) {
		Expect(prepare.Run(ctx, cfg, deps)).To(Succeed())

		expectContent(pathFile, pathSeed+binDir+"\n")
		expectContent(envFile, envSeed+"ROOKET_ENGINE=docker\n")
	})

	It("with ISCSI false, runs no command and opens no log group, and still exports", func(ctx SpecContext) {
		cfg.ISCSI = false

		Expect(prepare.Run(ctx, cfg, deps)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(0), "runner calls")
		Expect(out.String()).To(BeEmpty())
		expectContent(pathFile, pathSeed+binDir+"\n")
		expectContent(envFile, envSeed+"ROOKET_ENGINE=docker\n")
	})

	It("stops at a refused guard before touching anything", func(ctx SpecContext) {
		env["RUNNER_ENVIRONMENT"] = "self-hosted"

		Expect(prepare.Run(ctx, cfg, deps)).To(MatchError(ContainSubstring("RUNNER_ENVIRONMENT")))

		expectUntouched()
	})

	DescribeTable("requires each file it exports through before touching anything",
		func(ctx SpecContext, name string) {
			delete(env, name)

			Expect(prepare.Run(ctx, cfg, deps)).To(MatchError(ContainSubstring(name)))

			expectUntouched()
		},
		Entry("GITHUB_PATH unset", "GITHUB_PATH"),
		Entry("GITHUB_ENV unset", "GITHUB_ENV"),
	)

	It("refuses a relative BinDir before touching anything, because GITHUB_PATH needs an absolute one", func(ctx SpecContext) {
		// A regression would create BinDir here rather than in the source tree.
		GinkgoT().Chdir(GinkgoT().TempDir())
		cfg.BinDir = filepath.Join("rooket-action", "bin")

		Expect(prepare.Run(ctx, cfg, deps)).To(MatchError(ContainSubstring(cfg.BinDir)))

		expectUntouched()
		Expect(cfg.BinDir).NotTo(BeADirectory())
	})

	It("writes neither file when a download fails, and sets up no iSCSI", func(ctx SpecContext) {
		deps.Pins[1].URL += "-missing"

		Expect(prepare.Run(ctx, cfg, deps)).To(MatchError(ContainSubstring("kubectl")))

		Expect(runner.RunCallCount()).To(Equal(0), "runner calls")
		expectContent(pathFile, pathSeed)
		expectContent(envFile, envSeed)
	})

	It("writes neither file when an iSCSI command fails, and closes the log group", func(ctx SpecContext) {
		cause := errors.New("exit status 1")
		runner.RunReturnsOnCall(2, cause)

		Expect(prepare.Run(ctx, cfg, deps)).To(MatchError(cause))

		Expect(runner.RunCallCount()).To(Equal(3), "runner calls")
		Expect(out.String()).To(Equal("::group::Installing iSCSI and LVM tooling\n::endgroup::\n"))
		expectContent(pathFile, pathSeed)
		expectContent(envFile, envSeed)
	})

	DescribeTable("sizes the disk step by Workers and MinFreeGiB, and carries on with a warning on Out when still short",
		func(ctx SpecContext, workers, minFreeGiB int, needGiB string) {
			cfg.Workers, cfg.MinFreeGiB, cfg.ISCSI = workers, minFreeGiB, false
			free = 20 << 30

			Expect(prepare.Run(ctx, cfg, deps)).To(Succeed())

			Expect(out.String()).To(Equal(
				"::warning::20.0 GiB free on / after deleting preinstalled toolchains; " + needGiB + " GiB needed\n"))
			Expect(runner.RunCallCount()).To(Equal(1), "runner calls")
			expectContent(pathFile, pathSeed+binDir+"\n")
		},
		Entry("three workers by the formula", 3, 0, "60.0"),
		Entry("min-free-gb overriding the formula", 3, 25, "25.0"),
	)
})
