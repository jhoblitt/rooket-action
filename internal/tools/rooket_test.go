package tools_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/tools"
)

var _ = Describe("Rooket", func() {
	DescribeTable("builds the pin from rooket's archive name template",
		func(version string) {
			h := sha([]byte("rooket archive"))

			Expect(tools.Rooket(version, h)).To(Equal(tools.Pin{
				Name:    "rooket",
				Version: "v1.2.3",
				URL:     "https://github.com/jhoblitt/rooket/releases/download/v1.2.3/rooket_1.2.3_linux_amd64.tar.gz",
				SHA256:  h,
				Member:  "rooket",
			}), "version %q", version)
		},
		Entry("a version with its leading v", "v1.2.3"),
		Entry("a version without a leading v", "1.2.3"),
	)
})

var _ = Describe("DefaultRooket", func() {
	It("pins a release below v2, which rooket's unsuffixed module path requires", func() {
		Expect(tools.DefaultRooket().Version).To(MatchRegexp(`^v[01]\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`))
	})

	It("carries a sha256 and the second source that confirmed it", func() {
		p := tools.DefaultRooket()

		Expect(p.SHA256).To(MatchRegexp(`^[0-9a-f]{64}$`))
		Expect(p.SecondSource).NotTo(BeEmpty())
	})

	It("is Rooket applied to the compiled-in version and hash", func() {
		p := tools.DefaultRooket()
		p.SecondSource = ""

		Expect(p).To(Equal(tools.Rooket(p.Version, p.SHA256)))
	})
})
