package tools_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/tools"
)

const nodeDigest = "sha256:050072256b9a903bd914c0b2866828150cb229cea0efe5892e2b644d5dd3b34f"

// rooketModule writes decls into a fresh module directory's
// cmd/nodeimage.go, and returns the directory and the file.
func rooketModule(decls string) (dir, file string) {
	dir = GinkgoT().TempDir()
	file = filepath.Join(dir, "cmd", "nodeimage.go")
	Expect(os.Mkdir(filepath.Dir(file), 0o755)).To(Succeed())
	Expect(os.WriteFile(file, []byte("package cmd\n\n"+decls+"\n"), 0o644)).To(Succeed())
	return dir, file
}

var _ = Describe("NodeImageTag", func() {
	DescribeTable("returns the tag of the defaultNodeImage constant",
		func(decls string) {
			dir, _ := rooketModule(decls)

			Expect(tools.NodeImageTag(dir)).To(Equal("v1.33.1"))
		},
		Entry("in rooket's digest-pinned form",
			`const defaultNodeImage = "kindest/node:v1.33.1@`+nodeDigest+`"`),
		Entry("declared in a const group",
			"const (\n\tkindVersion = \"v0.29.0\"\n\tdefaultNodeImage = \"kindest/node:v1.33.1\"\n)"),
		Entry("from a registry with a port",
			`const defaultNodeImage = "localhost:5000/kindest/node:v1.33.1"`),
	)

	DescribeTable("is an error naming the file, defaultNodeImage and the cause",
		func(decls, cause string) {
			dir, file := rooketModule(decls)

			_, err := tools.NodeImageTag(dir)

			Expect(err).To(MatchError(And(
				ContainSubstring(file),
				ContainSubstring("defaultNodeImage"),
				ContainSubstring(cause),
			)))
		},
		Entry("when rooket renames the constant",
			`const nodeImage = "kindest/node:v1.33.1@`+nodeDigest+`"`,
			"no such constant"),
		Entry("when the constant is not a string literal",
			"const defaultNodeImage = image + \":v1.33.1\"\n\nconst image = \"kindest/node\"",
			"not a string literal"),
		Entry("when the image has no tag",
			`const defaultNodeImage = "localhost:5000/kindest/node@`+nodeDigest+`"`,
			`"localhost:5000/kindest/node@`+nodeDigest+`" has no tag`),
	)

	It("is an error naming the file and defaultNodeImage when rooket moves the file", func() {
		dir := GinkgoT().TempDir()

		_, err := tools.NodeImageTag(dir)

		Expect(err).To(MatchError(And(
			ContainSubstring(filepath.Join(dir, "cmd", "nodeimage.go")),
			ContainSubstring("defaultNodeImage"),
		)))
	})
})

// moduleDir returns the source directory of path@version that
// `go mod download` fetches through the module proxy. It runs outside this
// module, so the download leaves go.mod and go.sum alone.
func moduleDir(ctx SpecContext, path, version string) string {
	GinkgoHelper()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "go", "mod", "download", "-json", path+"@"+version)
	cmd.Dir = GinkgoT().TempDir()
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	Expect(err).NotTo(HaveOccurred(), "%s: %s%s", cmd, stdout.String(), stderr.String())
	var mod struct{ Dir, Error string }
	Expect(json.Unmarshal(stdout.Bytes(), &mod)).To(Succeed(), "%s: %s", cmd, stdout.String())
	Expect(mod.Error).To(BeEmpty(), "%s", cmd)
	Expect(mod.Dir).NotTo(BeEmpty(), "%s: %s", cmd, stdout.String())
	return mod.Dir
}

func pinNamed(name string) tools.Pin {
	GinkgoHelper()
	for _, p := range tools.Pins() {
		if p.Name == name {
			return p
		}
	}
	Fail("no pin named " + name)
	return tools.Pin{}
}

// minor returns vMAJOR.MINOR of the version v.
func minor(v string) string {
	GinkgoHelper()
	Expect(v).To(MatchRegexp(`^v[0-9]+\.[0-9]+\.[0-9]+`), "version %q", v)
	parts := strings.SplitN(v, ".", 3)
	return parts[0] + "." + parts[1]
}

var _ = Describe("the kubectl pin", func() {
	It("has the minor of the node image the pinned rooket release defaults to", func(ctx SpecContext) {
		rooket := tools.DefaultRooket()
		tag, err := tools.NodeImageTag(moduleDir(ctx, "github.com/jhoblitt/rooket", rooket.Version))
		Expect(err).NotTo(HaveOccurred())
		kubectl := pinNamed("kubectl")

		Expect(minor(kubectl.Version)).To(Equal(minor(tag)),
			"rooket %s defaults to node image %s, but kubectl is pinned at %s: bump kubectl's pin, with its hash, in internal/tools/pins.go",
			rooket.Version, tag, kubectl.Version)
	}, SpecTimeout(2*time.Minute))
})
