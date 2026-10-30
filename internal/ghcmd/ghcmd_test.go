package ghcmd_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/ghcmd"
)

var _ = Describe("Warning", func() {
	It("writes msg as one ::warning:: workflow command line", func() {
		var out bytes.Buffer

		ghcmd.Warning(&out, "disk is short")

		Expect(out.String()).To(Equal("::warning::disk is short\n"))
	})

	It("escapes percent signs, carriage returns and newlines so the warning stays one workflow command", func() {
		var out bytes.Buffer

		ghcmd.Warning(&out, "100% broken\nsecond line\rthird")

		Expect(out.String()).To(Equal("::warning::100%25 broken%0Asecond line%0Dthird\n"))
	})
})
