package ghfiles_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/ghfiles"
)

// seeded writes data to dir/file and returns that path.
func seeded(dir, data string) string {
	GinkgoHelper()
	path := filepath.Join(dir, "file")
	Expect(os.WriteFile(path, []byte(data), 0o600)).To(Succeed())
	return path
}

func content(path string) string {
	GinkgoHelper()
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}

var _ = Describe("ghfiles", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	It("Set appends key=value after the lines already there", func() {
		path := seeded(dir, "FOO=bar\n")

		Expect(ghfiles.Set(path, "ROOKET_ENGINE", "docker")).To(Succeed())

		Expect(content(path)).To(Equal("FOO=bar\nROOKET_ENGINE=docker\n"))
	})

	It("AddPath appends dir after the lines already there", func() {
		path := seeded(dir, "/usr/local/bin\n")

		Expect(ghfiles.AddPath(path, "/runner/temp/rooket-action/bin")).To(Succeed())

		Expect(content(path)).To(Equal("/usr/local/bin\n/runner/temp/rooket-action/bin\n"))
	})

	DescribeTable("creates a file that does not exist yet",
		func(write func(path string) error, want string) {
			path := filepath.Join(dir, "missing")

			Expect(write(path)).To(Succeed())

			Expect(content(path)).To(Equal(want))
		},
		Entry("Set", func(p string) error { return ghfiles.Set(p, "path", "/bin/rooket") }, "path=/bin/rooket\n"),
		Entry("AddPath", func(p string) error { return ghfiles.AddPath(p, "/bin") }, "/bin\n"),
	)

	DescribeTable("rejects a newline before opening the file, naming the argument",
		func(write func(path string) error, arg string) {
			path := filepath.Join(dir, "missing")

			Expect(write(path)).To(MatchError(And(ContainSubstring(arg), ContainSubstring("newline"))))

			Expect(path).NotTo(BeAnExistingFile())
		},
		Entry("a value holding LF", func(p string) error { return ghfiles.Set(p, "KEY", "one\nTWO=two") }, "value"),
		Entry("a value holding CR", func(p string) error { return ghfiles.Set(p, "KEY", "one\rTWO=two") }, "value"),
		Entry("a key holding LF", func(p string) error { return ghfiles.Set(p, "ONE=one\nKEY", "v") }, "key"),
		Entry("a dir holding LF", func(p string) error { return ghfiles.AddPath(p, "/bin\n/evil") }, "directory"),
	)

	It("names the file it cannot open", func() {
		path := filepath.Join(dir, "no-such-dir", "file")

		Expect(ghfiles.Set(path, "KEY", "v")).To(MatchError(ContainSubstring(path)))
	})
})
