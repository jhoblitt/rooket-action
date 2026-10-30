package cli_test

import (
	"bytes"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/cli"
	"github.com/jhoblitt/rooket-action/internal/version"
)

var _ = Describe("Run", func() {
	var stdout, stderr *bytes.Buffer

	BeforeEach(func() {
		stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
	})

	It("prints the build's version for --version", func(ctx SpecContext) {
		Expect(cli.Run(ctx, []string{"--version"}, strings.NewReader(""), stdout, stderr)).To(Succeed())

		Expect(stdout.String()).To(Equal("rooket-action version " + version.String() + "\n"))
	})

	It("offers the setup and teardown subcommands", func(ctx SpecContext) {
		Expect(cli.Run(ctx, []string{"help"}, strings.NewReader(""), stdout, stderr)).To(Succeed())

		Expect(stdout.String()).To(And(
			MatchRegexp(`(?m)^\s+setup\s`),
			MatchRegexp(`(?m)^\s+teardown\s`),
		))
	})

	It("rejects an argument to a subcommand", func(ctx SpecContext) {
		err := cli.Run(ctx, []string{"teardown", "extra"}, strings.NewReader(""), stdout, stderr)

		Expect(err).To(MatchError(ContainSubstring(`unknown command "extra"`)))
	})
})
