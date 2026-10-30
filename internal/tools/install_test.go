package tools_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/tools"
)

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// quick keeps the suite fast: a millisecond backoff, and a per-request
// timeout no spec that expects success comes near.
func quick(attempts int) tools.Retry {
	return tools.Retry{Attempts: attempts, PerRequest: 5 * time.Second, Backoff: time.Millisecond}
}

// serve starts a server that hands h each request's 1-based ordinal, and
// returns it with the count of requests made so far.
func serve(h func(n int32, w http.ResponseWriter, r *http.Request)) (*httptest.Server, *atomic.Int32) {
	hits := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h(hits.Add(1), w, r)
	}))
	DeferCleanup(srv.Close)
	return srv, hits
}

func send(b []byte) func(int32, http.ResponseWriter, *http.Request) {
	return func(_ int32, w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
	}
}

func unavailable(w http.ResponseWriter) {
	http.Error(w, "try later", http.StatusServiceUnavailable)
}

type member struct {
	name string
	data []byte
}

func tarGz(members ...member) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, m := range members {
		Expect(tw.WriteHeader(&tar.Header{
			Name:     m.name,
			Mode:     0o755,
			Size:     int64(len(m.data)),
			Typeflag: tar.TypeReg,
		})).To(Succeed())
		Expect(tw.Write(m.data)).To(Equal(len(m.data)))
	}
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

func entries(dir string) []string {
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

var _ = Describe("Install", func() {
	var binDir string

	BeforeEach(func() {
		binDir = GinkgoT().TempDir()

		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(GinkgoWriter, nil)))
		DeferCleanup(slog.SetDefault, prev)
	})

	It("installs the pinned bytes at binDir/<Name> with mode 0755", func(ctx SpecContext) {
		bin := []byte("kind binary")
		srv, hits := serve(send(bin))
		pin := tools.Pin{Name: "kind", URL: srv.URL + "/kind", SHA256: sha(bin)}

		Expect(tools.Install(ctx, srv.Client(), pin, binDir, quick(3))).To(Succeed())

		expectInstalled(filepath.Join(binDir, "kind"), bin)
		Expect(entries(binDir)).To(ConsistOf("kind"))
		Expect(hits.Load()).To(Equal(int32(1)))
	})

	It("refuses a sha256 mismatch after one request, naming the pin and both hashes", func(ctx SpecContext) {
		bin := []byte("tampered kind binary")
		want := sha([]byte("kind binary"))
		srv, hits := serve(send(bin))
		pin := tools.Pin{Name: "kind", URL: srv.URL + "/kind", SHA256: want}

		err := tools.Install(ctx, srv.Client(), pin, binDir, quick(3))

		Expect(err).To(MatchError(And(
			ContainSubstring("kind"),
			ContainSubstring(want),
			ContainSubstring(sha(bin)),
		)))
		Expect(hits.Load()).To(Equal(int32(1)))
		Expect(entries(binDir)).To(BeEmpty())
	})

	It("retries a 503 and installs once the server recovers", func(ctx SpecContext) {
		bin := []byte("kubectl binary")
		srv, hits := serve(func(n int32, w http.ResponseWriter, r *http.Request) {
			if n <= 2 {
				unavailable(w)
				return
			}
			send(bin)(n, w, r)
		})
		pin := tools.Pin{Name: "kubectl", URL: srv.URL + "/kubectl", SHA256: sha(bin)}

		Expect(tools.Install(ctx, srv.Client(), pin, binDir, quick(3))).To(Succeed())

		expectInstalled(filepath.Join(binDir, "kubectl"), bin)
		Expect(hits.Load()).To(Equal(int32(3)))
	})

	It("gives up after Attempts failures, naming the URL", func(ctx SpecContext) {
		srv, hits := serve(func(_ int32, w http.ResponseWriter, _ *http.Request) {
			unavailable(w)
		})
		pin := tools.Pin{Name: "kubectl", URL: srv.URL + "/kubectl", SHA256: sha([]byte("kubectl binary"))}

		err := tools.Install(ctx, srv.Client(), pin, binDir, quick(3))

		Expect(err).To(MatchError(ContainSubstring(pin.URL)))
		Expect(hits.Load()).To(Equal(int32(3)))
		Expect(entries(binDir)).To(BeEmpty())
	})

	It("counts a request that outlasts PerRequest as a failed attempt, and retries it", func(ctx SpecContext) {
		bin := []byte("kind binary")
		srv, hits := serve(func(n int32, w http.ResponseWriter, r *http.Request) {
			if n == 1 {
				<-r.Context().Done()
				return
			}
			send(bin)(n, w, r)
		})
		pin := tools.Pin{Name: "kind", URL: srv.URL + "/kind", SHA256: sha(bin)}
		r := tools.Retry{Attempts: 3, PerRequest: 250 * time.Millisecond, Backoff: time.Millisecond}

		Expect(tools.Install(ctx, srv.Client(), pin, binDir, r)).To(Succeed())

		expectInstalled(filepath.Join(binDir, "kind"), bin)
		Expect(hits.Load()).To(Equal(int32(2)))
	}, SpecTimeout(10*time.Second))

	It("stops retrying when its context is canceled", func(sctx SpecContext) {
		ctx, cancel := context.WithCancel(sctx)
		DeferCleanup(cancel)
		srv, hits := serve(func(_ int32, w http.ResponseWriter, _ *http.Request) {
			cancel()
			unavailable(w)
		})
		pin := tools.Pin{Name: "kind", URL: srv.URL + "/kind", SHA256: sha([]byte("kind binary"))}
		r := tools.Retry{Attempts: 3, PerRequest: 5 * time.Second, Backoff: time.Hour}

		err := tools.Install(ctx, srv.Client(), pin, binDir, r)

		Expect(err).To(MatchError(context.Canceled))
		Expect(hits.Load()).To(Equal(int32(1)))
		Expect(entries(binDir)).To(BeEmpty())
	}, SpecTimeout(10*time.Second))

	Context("with an archive member", func() {
		It("installs only that member", func(ctx SpecContext) {
			helm := []byte("helm binary")
			archive := tarGz(
				member{"linux-amd64/LICENSE", []byte("license")},
				member{"linux-amd64/helm", helm},
				member{"linux-amd64/README.md", []byte("readme")},
			)
			srv, _ := serve(send(archive))
			pin := tools.Pin{
				Name:   "helm",
				URL:    srv.URL + "/helm.tar.gz",
				SHA256: sha(archive),
				Member: "linux-amd64/helm",
			}

			Expect(tools.Install(ctx, srv.Client(), pin, binDir, quick(3))).To(Succeed())

			expectInstalled(filepath.Join(binDir, "helm"), helm)
			Expect(entries(binDir)).To(ConsistOf("helm"))
		})

		It("is an error naming the member when the archive lacks it", func(ctx SpecContext) {
			archive := tarGz(
				member{"linux-amd64/LICENSE", []byte("license")},
				member{"linux-amd64/README.md", []byte("readme")},
			)
			srv, _ := serve(send(archive))
			pin := tools.Pin{
				Name:   "helm",
				URL:    srv.URL + "/helm.tar.gz",
				SHA256: sha(archive),
				Member: "linux-amd64/helm",
			}

			err := tools.Install(ctx, srv.Client(), pin, binDir, quick(3))

			Expect(err).To(MatchError(ContainSubstring("linux-amd64/helm")))
			Expect(entries(binDir)).To(BeEmpty())
		})
	})

	It("replaces an installed copy without truncating it under a reader", func(ctx SpecContext) {
		first, second := []byte("kind v1"), []byte("kind v2, longer")
		var body atomic.Pointer[[]byte]
		body.Store(&first)
		srv, _ := serve(func(n int32, w http.ResponseWriter, r *http.Request) {
			send(*body.Load())(n, w, r)
		})
		pin := tools.Pin{Name: "kind", URL: srv.URL + "/kind", SHA256: sha(first)}
		dst := filepath.Join(binDir, "kind")

		Expect(tools.Install(ctx, srv.Client(), pin, binDir, quick(3))).To(Succeed())
		running, err := os.Open(dst)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(running.Close)

		body.Store(&second)
		pin.SHA256 = sha(second)
		Expect(tools.Install(ctx, srv.Client(), pin, binDir, quick(3))).To(Succeed())

		expectInstalled(dst, second)
		Expect(io.ReadAll(running)).To(Equal(first), "the copy open before the second install")
		Expect(entries(binDir)).To(ConsistOf("kind"))
	})
})

var _ = Describe("Pins", func() {
	It("pins kind, kubectl and helm, each with a sha256 and a second source", func() {
		pins := tools.Pins()

		names := make([]string, 0, len(pins))
		for _, p := range pins {
			names = append(names, p.Name)
			Expect(p.SHA256).To(MatchRegexp(`^[0-9a-f]{64}$`), "SHA256 of pin %s", p.Name)
			Expect(p.SecondSource).NotTo(BeEmpty(), "SecondSource of pin %s", p.Name)
		}
		Expect(names).To(ConsistOf("kind", "kubectl", "helm"))
	})
})
