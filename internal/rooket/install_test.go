package rooket_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/jhoblitt/rooket-action/internal/rooket"
	"github.com/jhoblitt/rooket-action/internal/rooket/rooketfakes"
	"github.com/jhoblitt/rooket-action/internal/sys"
	"github.com/jhoblitt/rooket-action/internal/tools"
)

// sys.Exec is the Runner production hands rooket.
var _ rooket.Runner = sys.Exec{}

// A well-formed sha256 that no archive in these specs has: the empty
// string's.
const otherHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

const goBin = "/opt/hostedtoolcache/go/1.27.0/x64/bin/go"

func asIs(h string) string { return h }

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// archive returns a gzipped tar archive holding bin as the member rooket,
// the shape of rooket's linux/amd64 release archive.
func archive(bin []byte) []byte {
	GinkgoHelper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	Expect(tw.WriteHeader(&tar.Header{Name: "rooket", Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg})).To(Succeed())
	Expect(tw.Write(bin)).To(Equal(len(bin)))
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

func urlPath(raw string) string {
	GinkgoHelper()
	u, err := url.Parse(raw)
	Expect(err).NotTo(HaveOccurred())
	return u.Path
}

func entries(dir string) []string {
	GinkgoHelper()
	des, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(des))
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}

func expectInstalled(path string, want []byte) {
	GinkgoHelper()
	Expect(os.ReadFile(path)).To(Equal(want), "content of %s", path)
	info, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	Expect(info.Mode().Perm()).To(Equal(fs.FileMode(0o755)), "mode of %s", path)
}

// reroute sends every request to the server at to, keeping its path, so
// that a pin's real GitHub URL can be served by a test server. It records
// the URL each request was made for.
type reroute struct {
	to   *url.URL
	next http.RoundTripper

	mu   sync.Mutex
	urls []string
}

func (t *reroute) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.urls = append(t.urls, req.URL.String())
	t.mu.Unlock()
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host, out.Host = t.to.Scheme, t.to.Host, ""
	return t.next.RoundTrip(out)
}

func (t *reroute) requested() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.urls)
}

var _ = Describe("Install", func() {
	var (
		runner *rooketfakes.FakeRunner
		binDir string
		files  map[string][]byte
		hits   *atomic.Int32
		rt     *reroute
		client *http.Client
		retry  tools.Retry
	)

	expectNoCommands := func() {
		GinkgoHelper()
		Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
		Expect(runner.OutputCallCount()).To(BeZero(), "Output calls")
	}

	BeforeEach(func() {
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(GinkgoWriter, nil)))
		DeferCleanup(slog.SetDefault, prev)

		runner = new(rooketfakes.FakeRunner)
		binDir = GinkgoT().TempDir()
		files = map[string][]byte{}
		hits = new(atomic.Int32)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			b, ok := files[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
		}))
		DeferCleanup(srv.Close)
		to, err := url.Parse(srv.URL)
		Expect(err).NotTo(HaveOccurred())
		rt = &reroute{to: to, next: srv.Client().Transport}
		client = &http.Client{Transport: rt}
		// Three attempts, so that a spec expecting one request proves the
		// failure was not retried.
		retry = tools.Retry{Attempts: 3, PerRequest: 5 * time.Second, Backoff: time.Millisecond}
	})

	Context("with neither rooket-version nor rooket-sha256", func() {
		// No test can serve bytes matching the pinned hash, so this spec
		// proves the pin is used by the mismatch it reports. The specs for
		// a consumer release prove the same download installs rooket.
		It("downloads DefaultRooket's archive from GitHub and checks it against the pinned hash", func(ctx SpecContext) {
			pin := tools.DefaultRooket()
			served := archive([]byte("not the pinned release"))
			files[urlPath(pin.URL)] = served

			path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{Go: goBin, BinDir: binDir})

			Expect(err).To(MatchError(And(
				ContainSubstring("got "+sha(served)),
				ContainSubstring("want "+pin.SHA256),
			)))
			Expect(path).To(BeEmpty())
			Expect(rt.requested()).To(Equal([]string{pin.URL}))
			Expect(hits.Load()).To(Equal(int32(1)), "HTTP requests")
			expectNoCommands()
			Expect(entries(binDir)).To(BeEmpty())
		})

		It("treats a rooket-version of only whitespace as unset", func(ctx SpecContext) {
			pin := tools.DefaultRooket()
			files[urlPath(pin.URL)] = archive([]byte("not the pinned release"))

			_, err := rooket.Install(ctx, runner, client, retry, rooket.Options{Go: goBin, BinDir: binDir, Version: "  "})

			Expect(err).To(MatchError(ContainSubstring("want " + pin.SHA256)))
			Expect(rt.requested()).To(Equal([]string{pin.URL}))
			expectNoCommands()
		})
	})

	Context("with rooket-version and rooket-sha256", func() {
		DescribeTable("downloads that release's archive and installs rooket from it at mode 0755",
			func(ctx SpecContext, version string, hashInput func(string) string) {
				bin := []byte("rooket v1.2.3 binary")
				served := archive(bin)
				want := tools.Rooket("v1.2.3", sha(served))
				files[urlPath(want.URL)] = served

				path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{
					Go: goBin, BinDir: binDir, Version: version, SHA256: hashInput(sha(served)),
				})

				Expect(err).NotTo(HaveOccurred())
				Expect(path).To(Equal(filepath.Join(binDir, "rooket")))
				expectInstalled(path, bin)
				Expect(entries(binDir)).To(ConsistOf("rooket"))
				Expect(rt.requested()).To(Equal([]string{want.URL}))
				expectNoCommands()
			},
			Entry("a version with its leading v", "v1.2.3", asIs),
			Entry("a version without its leading v", "1.2.3", asIs),
			Entry("both padded with whitespace, the hash in upper case", "  v1.2.3 \n",
				func(h string) string { return " " + strings.ToUpper(h) + "\n" }),
		)

		It("fails naming both hashes after one request when the archive does not match, using the hash in lower case", func(ctx SpecContext) {
			served := archive([]byte("rooket"))
			files[urlPath(tools.Rooket("v1.2.3", otherHash).URL)] = served

			path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{
				Go: goBin, BinDir: binDir, Version: "v1.2.3", SHA256: strings.ToUpper(otherHash),
			})

			Expect(err).To(MatchError(And(
				ContainSubstring("got "+sha(served)),
				ContainSubstring("want "+otherHash),
			)))
			Expect(path).To(BeEmpty())
			Expect(hits.Load()).To(Equal(int32(1)), "HTTP requests")
			Expect(entries(binDir)).To(BeEmpty())
		})
	})

	Context("with rooket-version alone", func() {
		DescribeTable("runs go install for it, trimmed and otherwise verbatim, into BinDir, shielded from the parent's Go environment",
			func(ctx SpecContext, version, want string) {
				path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{Go: goBin, BinDir: binDir, Version: version})

				Expect(err).NotTo(HaveOccurred())
				Expect(path).To(Equal(filepath.Join(binDir, "rooket")))
				Expect(runner.RunCallCount()).To(Equal(1), "Run calls")
				_, env, name, args := runner.RunArgsForCall(0)
				Expect(name).To(Equal(goBin))
				Expect(args).To(Equal([]string{"install", "github.com/jhoblitt/rooket@" + want}))
				Expect(env).To(ConsistOf("GOBIN="+binDir, "GOTOOLCHAIN=auto", "GOWORK=off", "GOFLAGS=", "GOOS=", "GOARCH=",
					"GOENV=off", "GOSUMDB=sum.golang.org", "GONOSUMDB=", "GOPRIVATE="))
				Expect(hits.Load()).To(BeZero(), "HTTP requests")
			},
			Entry("a release version", "v1.2.3", "v1.2.3"),
			Entry("a commit SHA padded with spaces",
				"  0123456789abcdef0123456789abcdef01234567  ", "0123456789abcdef0123456789abcdef01234567"),
			Entry("a branch", "main", "main"),
			Entry("a pseudo-version", "v0.1.1-0.20261029120000-0123456789ab", "v0.1.1-0.20261029120000-0123456789ab"),
		)

		DescribeTable("overrides the parent environment's Go setting",
			func(ctx SpecContext, key, parent, want string) {
				GinkgoT().Setenv(key, parent)

				_, err := rooket.Install(ctx, runner, client, retry, rooket.Options{Go: goBin, BinDir: binDir, Version: "main"})

				Expect(err).NotTo(HaveOccurred())
				Expect(runner.RunCallCount()).To(Equal(1), "Run calls")
				_, env, _, _ := runner.RunArgsForCall(0)
				Expect(env).To(ContainElement(want), "env with the parent's %s=%s", key, parent)
			},
			Entry("a toolchain policy that blocks the fetch", "GOTOOLCHAIN", "local", "GOTOOLCHAIN=auto"),
			Entry("a workspace file", "GOWORK", "/x/go.work", "GOWORK=off"),
			Entry("vendoring flags", "GOFLAGS", "-mod=vendor", "GOFLAGS="),
			Entry("a cross-compilation OS", "GOOS", "windows", "GOOS="),
			Entry("a cross-compilation architecture", "GOARCH", "arm64", "GOARCH="),
			Entry("a go env -w file", "GOENV", "/x/go/env", "GOENV=off"),
			Entry("the checksum database turned off", "GOSUMDB", "off", "GOSUMDB=sum.golang.org"),
			Entry("rooket's module marked private, which skips the checksum database",
				"GOPRIVATE", "github.com/jhoblitt/*", "GOPRIVATE="),
			Entry("rooket's module excluded from the checksum database",
				"GONOSUMDB", "github.com/jhoblitt/*", "GONOSUMDB="),
		)

		// go reads a variable set to "" as unset and falls back to the
		// go env -w file, so emptying GOFLAGS is not enough on its own. This
		// runs the go that runs the specs, which go test puts first on PATH.
		It("keeps the parent's go env -w settings out of go install", func(ctx SpecContext) {
			goTool, err := exec.LookPath("go")
			Expect(err).NotTo(HaveOccurred())
			envFile := filepath.Join(GinkgoT().TempDir(), "env")
			Expect(os.WriteFile(envFile, []byte(
				"GOFLAGS=-mod=vendor\nGOSUMDB=off\nGOPRIVATE=github.com/jhoblitt/*\nGONOSUMDB=github.com/jhoblitt/*\n"), 0o600)).To(Succeed())
			GinkgoT().Setenv("GOENV", envFile)
			for _, key := range []string{"GOFLAGS", "GOSUMDB", "GOPRIVATE", "GONOSUMDB"} {
				GinkgoT().Setenv(key, "")
			}
			goEnv := func(env []string) []string {
				GinkgoHelper()
				out, goErr := sys.Exec{Stderr: GinkgoWriter}.Output(ctx, GinkgoT().TempDir(), env,
					goTool, "env", "GOFLAGS", "GOSUMDB", "GOPRIVATE", "GONOSUMDB")
				Expect(goErr).NotTo(HaveOccurred())
				return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
			}
			Expect(goEnv(nil)).To(Equal([]string{"-mod=vendor", "off", "github.com/jhoblitt/*", "github.com/jhoblitt/*"}),
				"go env in the parent's environment, which reads the file")

			_, err = rooket.Install(ctx, runner, client, retry, rooket.Options{Go: goTool, BinDir: binDir, Version: "main"})

			Expect(err).NotTo(HaveOccurred())
			Expect(runner.RunCallCount()).To(Equal(1), "Run calls")
			_, env, _, _ := runner.RunArgsForCall(0)
			Expect(goEnv(env)).To(Equal([]string{"", "sum.golang.org", "", ""}),
				"go env in go install's environment")
		})

		It("returns go install's error", func(ctx SpecContext) {
			cause := errors.New("exit status 1")
			runner.RunReturns(cause)

			path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{Go: goBin, BinDir: binDir, Version: "main"})

			Expect(err).To(MatchError(cause))
			Expect(path).To(BeEmpty())
		})
	})

	Context("with rooket-path", func() {
		It("copies a relative path that lost its exec bit from the working directory, at mode 0755", func(ctx SpecContext) {
			cwd := GinkgoT().TempDir()
			GinkgoT().Chdir(cwd)
			bin := []byte("locally built rooket")
			Expect(os.WriteFile(filepath.Join(cwd, "rooket"), bin, 0o644)).To(Succeed())

			path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{Go: goBin, BinDir: binDir, Path: "./rooket"})

			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(binDir, "rooket")))
			expectInstalled(path, bin)
			Expect(entries(binDir)).To(ConsistOf("rooket"))
			expectNoCommands()
			Expect(hits.Load()).To(BeZero(), "HTTP requests")
		})

		It("replaces a rooket already in BinDir, as a second use of the action in one job does", func(ctx SpecContext) {
			src := filepath.Join(GinkgoT().TempDir(), "rooket")
			for _, bin := range [][]byte{[]byte("first rooket"), []byte("second rooket")} {
				Expect(os.WriteFile(src, bin, 0o644)).To(Succeed())

				path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{BinDir: binDir, Path: src})

				Expect(err).NotTo(HaveOccurred())
				expectInstalled(path, bin)
			}
			Expect(entries(binDir)).To(ConsistOf("rooket"))
		})

		It("keeps the binary intact when rooket-path is BinDir/rooket itself", func(ctx SpecContext) {
			dst := filepath.Join(binDir, "rooket")
			bin := []byte("installed rooket")
			Expect(os.WriteFile(dst, bin, 0o755)).To(Succeed())

			path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{BinDir: binDir, Path: dst})

			Expect(err).NotTo(HaveOccurred())
			expectInstalled(path, bin)
			Expect(entries(binDir)).To(ConsistOf("rooket"))
		})

		It("fails naming a source that does not exist, and leaves BinDir empty", func(ctx SpecContext) {
			src := filepath.Join(GinkgoT().TempDir(), "missing")

			path, err := rooket.Install(ctx, runner, client, retry, rooket.Options{BinDir: binDir, Path: src})

			Expect(err).To(And(MatchError(ContainSubstring(src)), MatchError(fs.ErrNotExist)))
			Expect(path).To(BeEmpty())
			Expect(entries(binDir)).To(BeEmpty())
		})
	})

	DescribeTable("refuses inputs that conflict or are malformed, naming each, before touching anything",
		func(ctx SpecContext, o rooket.Options, names ...string) {
			o.Go, o.BinDir = goBin, binDir
			matchers := make([]types.GomegaMatcher, 0, len(names))
			for _, n := range names {
				matchers = append(matchers, ContainSubstring(n))
			}

			path, err := rooket.Install(ctx, runner, client, retry, o)

			Expect(err).To(MatchError(And(matchers...)))
			Expect(path).To(BeEmpty())
			Expect(o.Validate()).To(MatchError(err.Error()), "Validate agrees with Install")
			expectNoCommands()
			Expect(hits.Load()).To(BeZero(), "HTTP requests")
			Expect(entries(binDir)).To(BeEmpty())
		},
		Entry("rooket-sha256 alone",
			rooket.Options{SHA256: otherHash}, "input rooket-sha256", "rooket-version"),
		Entry("rooket-path with rooket-version",
			rooket.Options{Path: "./rooket", Version: "v1.2.3"}, "input rooket-path", "rooket-version"),
		Entry("rooket-path with rooket-sha256",
			rooket.Options{Path: "./rooket", SHA256: otherHash}, "input rooket-path", "rooket-sha256"),
		Entry("a rooket-sha256 one digit short",
			rooket.Options{Version: "v1.2.3", SHA256: otherHash[1:]}, "input rooket-sha256", `"`+otherHash[1:]+`"`),
		Entry("a rooket-sha256 holding a non-hex digit",
			rooket.Options{Version: "v1.2.3", SHA256: "g" + otherHash[1:]}, "input rooket-sha256"),
		Entry("a branch as rooket-version with rooket-sha256",
			rooket.Options{Version: "main", SHA256: otherHash}, "input rooket-version", `"main"`),
		Entry("a pseudo-version as rooket-version with rooket-sha256",
			rooket.Options{Version: "v0.1.1-0.20261029120000-0123456789ab", SHA256: otherHash}, "input rooket-version"),
		Entry("a rooket-version holding inner whitespace",
			rooket.Options{Version: "v1.2.3 v1.2.4"}, "input rooket-version", "whitespace"),
	)
})

var _ = Describe("Options", func() {
	DescribeTable("UsesGo reports whether Install runs go install, which needs Go",
		func(o rooket.Options, want bool) {
			Expect(o.UsesGo()).To(Equal(want))
		},
		Entry("rooket-version alone", rooket.Options{Version: "main"}, true),
		Entry("no inputs", rooket.Options{}, false),
		Entry("rooket-version with rooket-sha256", rooket.Options{Version: "v1.2.3", SHA256: otherHash}, false),
		Entry("rooket-path", rooket.Options{Path: "./rooket"}, false),
		Entry("rooket-version with rooket-path, an input error", rooket.Options{Version: "main", Path: "./rooket"}, false),
	)

	DescribeTable("Validate accepts each way of choosing rooket",
		func(o rooket.Options) {
			Expect(o.Validate()).To(Succeed())
		},
		Entry("no inputs", rooket.Options{}),
		Entry("rooket-version alone", rooket.Options{Version: "main"}),
		Entry("rooket-version with rooket-sha256", rooket.Options{Version: "1.2.3", SHA256: otherHash}),
		Entry("rooket-path", rooket.Options{Path: "./rooket"}),
	)
})

var _ = Describe("Version", func() {
	var runner *rooketfakes.FakeRunner

	BeforeEach(func() {
		runner = new(rooketfakes.FakeRunner)
	})

	It("returns the second word of the first line rooket version prints", func(ctx SpecContext) {
		runner.OutputReturns([]byte("rooket v0.1.0\ncommit 0123456789ab\n"), nil)

		got, err := rooket.Version(ctx, runner, "/bin/dir/rooket")

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("v0.1.0"))
		Expect(runner.OutputCallCount()).To(Equal(1), "Output calls")
		_, dir, env, name, args := runner.OutputArgsForCall(0)
		Expect(dir).To(BeEmpty())
		Expect(env).To(BeEmpty())
		Expect(name).To(Equal("/bin/dir/rooket"))
		Expect(args).To(Equal([]string{"version"}))
	})

	It("returns rooket version's error", func(ctx SpecContext) {
		cause := errors.New("exit status 2")
		runner.OutputReturns(nil, cause)

		_, err := rooket.Version(ctx, runner, "/bin/dir/rooket")

		Expect(err).To(MatchError(cause))
	})

	DescribeTable("fails on output it cannot read a version from, quoting the output",
		func(ctx SpecContext, out string) {
			runner.OutputReturns([]byte(out), nil)

			got, err := rooket.Version(ctx, runner, "/bin/dir/rooket")

			Expect(err).To(MatchError(ContainSubstring("%q", out)))
			Expect(got).To(BeEmpty())
		},
		Entry("no output", ""),
		Entry("one word", "rooket\n"),
		Entry("the version on the second line", "\nrooket v0.1.0\n"),
	)
})
