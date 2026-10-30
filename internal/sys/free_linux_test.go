package sys_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/sys"
)

var _ = Describe("Free", func() {
	It("reports the bytes available on a path's filesystem", func() {
		Expect(sys.Free(GinkgoT().TempDir())).To(BeNumerically(">", 0))
	})

	It("names the path it could not measure", func() {
		missing := filepath.Join(GinkgoT().TempDir(), "missing")

		_, err := sys.Free(missing)

		Expect(err).To(MatchError(ContainSubstring(missing)))
	})
})
