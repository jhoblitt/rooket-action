package sys_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/sys"
)

var _ = Describe("Exec", func() {
	var (
		stdout, stderr *bytes.Buffer
		x              sys.Exec
	)

	BeforeEach(func() {
		stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
		x = sys.Exec{Stdout: stdout, Stderr: stderr}
	})

	It("streams Run's child stdout and stderr to its writers", func(ctx SpecContext) {
		Expect(x.Run(ctx, nil, "sh", "-c", "printf out; printf err >&2")).To(Succeed())

		Expect(stdout.String()).To(Equal("out"))
		Expect(stderr.String()).To(Equal("err"))
	})

	It("runs Output in dir, capturing stdout and passing stderr to Stderr", func(ctx SpecContext) {
		dir := GinkgoT().TempDir()
		want, err := filepath.EvalSymlinks(dir)
		Expect(err).NotTo(HaveOccurred())

		out, err := x.Output(ctx, dir, nil, "sh", "-c", "pwd -P; printf err >&2")

		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(Equal(want + "\n"))
		Expect(stdout.String()).To(BeEmpty())
		Expect(stderr.String()).To(Equal("err"))
	})

	It("lets an extra env entry override the parent's value for the same key", func(ctx SpecContext) {
		GinkgoT().Setenv("GOTOOLCHAIN", "local")

		out, err := x.Output(ctx, "", []string{"GOTOOLCHAIN=auto"}, "sh", "-c", `printf %s "$GOTOOLCHAIN"`)

		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(Equal("auto"))
	})

	It("lets an extra env entry with an empty value override the parent's value", func(ctx SpecContext) {
		GinkgoT().Setenv("GOFLAGS", "-mod=vendor")

		out, err := x.Output(ctx, "", []string{"GOFLAGS="}, "sh", "-c", `printf '%s:%s' "${GOFLAGS+set}" "$GOFLAGS"`)

		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(Equal("set:"), "GOFLAGS in the child, as set-or-not:value")
	})

	It("layers extra env over the parent's environment rather than replacing it", func(ctx SpecContext) {
		GinkgoT().Setenv("ROOKET_ACTION_PARENT", "parent")

		Expect(x.Run(ctx, []string{"ROOKET_ACTION_EXTRA=extra"}, "sh", "-c",
			`printf '%s %s' "$ROOKET_ACTION_PARENT" "$ROOKET_ACTION_EXTRA"`)).To(Succeed())

		Expect(stdout.String()).To(Equal("parent extra"))
	})

	DescribeTable("names the command in a failure and keeps the exit status",
		func(ctx SpecContext, run func(context.Context, sys.Exec) error) {
			err := run(ctx, x)

			Expect(err).To(MatchError(ContainSubstring("sh -c exit 3")))
			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue(), "error %v is not an *exec.ExitError", err)
			Expect(exitErr.ExitCode()).To(Equal(3))
		},
		Entry("Run", func(ctx context.Context, x sys.Exec) error {
			return x.Run(ctx, nil, "sh", "-c", "exit 3")
		}),
		Entry("Output", func(ctx context.Context, x sys.Exec) error {
			_, err := x.Output(ctx, "", nil, "sh", "-c", "exit 3")
			return err
		}),
	)
})
