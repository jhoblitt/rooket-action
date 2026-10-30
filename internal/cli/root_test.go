package cli_test

import (
	"bytes"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/cli"
)

var _ = Describe("Run", func() {
	It("greets the --name value on stdout", func(ctx SpecContext) {
		var stdout, stderr bytes.Buffer

		err := cli.Run(ctx, []string{"--name", "rooket"}, strings.NewReader(""), &stdout, &stderr)

		Expect(err).NotTo(HaveOccurred())
		Expect(stdout.String()).To(Equal("hello, rooket\n"))
	})
})
