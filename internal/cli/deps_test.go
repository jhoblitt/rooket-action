package cli_test

import (
	"bytes"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/cli"
	"github.com/jhoblitt/rooket-action/internal/prepare"
	"github.com/jhoblitt/rooket-action/internal/rooket"
	"github.com/jhoblitt/rooket-action/internal/sys"
	"github.com/jhoblitt/rooket-action/internal/tools"
)

// What setup hands its Runner on to.
var (
	_ prepare.Runner = cli.Runner(nil)
	_ rooket.Runner  = cli.Runner(nil)
)

var _ = Describe("ProductionDeps", func() {
	var (
		stdout, stderr *bytes.Buffer
		d              cli.Deps
	)

	BeforeEach(func() {
		stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
		d = cli.ProductionDeps(stdout, stderr)
	})

	It("installs the pinned tools", func() {
		Expect(d.Pins).NotTo(BeEmpty())
		Expect(d.Pins).To(Equal(tools.Pins()))
	})

	It("runs commands through sys.Exec with their output on the command's streams", func() {
		Expect(d.Runner).To(And(
			BeAssignableToTypeOf(sys.Exec{}),
			HaveField("Stdout", BeIdenticalTo(stdout)),
			HaveField("Stderr", BeIdenticalTo(stderr)),
		))
	})

	It("downloads with an HTTP client, retrying as tools.DefaultRetry does", func() {
		Expect(d.HTTP).NotTo(BeNil())
		Expect(d.Retry).To(Equal(tools.DefaultRetry))
	})

	It("reads the process environment, the helper's own path, and the free disk space", func() {
		GinkgoT().Setenv("ROOKET_ACTION_SPEC", "set")
		exe, err := os.Executable()
		Expect(err).NotTo(HaveOccurred())

		Expect(d.Getenv("ROOKET_ACTION_SPEC")).To(Equal("set"))
		Expect(d.Executable()).To(Equal(exe))
		Expect(d.Free(GinkgoT().TempDir())).To(BeNumerically(">", 0))
	})
})
