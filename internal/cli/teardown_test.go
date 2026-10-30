package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/cli"
	"github.com/jhoblitt/rooket-action/internal/cli/clifakes"
)

var _ = Describe("teardown", func() {
	const rooketBin = "/home/runner/work/_temp/rooket-action/bin/rooket"

	var (
		runner         *clifakes.FakeRunner
		env            map[string]string
		stdout, stderr *bytes.Buffer
		d              cli.Deps
	)

	teardown := func(ctx context.Context, args ...string) error {
		return cli.RunWith(ctx, append([]string{"teardown"}, args...), stdout, stderr,
			func(io.Writer, io.Writer) cli.Deps { return d })
	}

	BeforeEach(func() {
		prev := slog.Default()
		DeferCleanup(slog.SetDefault, prev)

		runner = new(clifakes.FakeRunner)
		env = hosted()
		stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
		d = cli.Deps{Runner: runner, Getenv: func(key string) string { return env[key] }}
	})

	DescribeTable("runs exactly <rooket> down --all --force --delete-disks with ROOKET_ENGINE=docker",
		func(ctx SpecContext, flags ...string) {
			Expect(teardown(ctx, append([]string{"--rooket", rooketBin}, flags...)...)).To(Succeed())

			Expect(runner.RunCallCount()).To(Equal(1), "Run calls")
			_, env, name, args := runner.RunArgsForCall(0)
			Expect(env).To(Equal([]string{"ROOKET_ENGINE=docker"}))
			Expect(name).To(Equal(rooketBin))
			Expect(args).To(Equal([]string{"down", "--all", "--force", "--delete-disks"}))
			Expect(stdout.String()).To(BeEmpty())
		},
		Entry("--teardown true", "--teardown", "true"),
		Entry("--teardown TRUE", "--teardown", "TRUE"),
		Entry("--teardown omitted, which defaults to true"),
	)

	It("prints one ::warning:: line and exits 0 when the teardown fails", func(ctx SpecContext) {
		runner.RunReturns(errors.New("running rooket down: exit status 1\ncluster smoke: still running"))

		Expect(teardown(ctx, "--rooket", rooketBin)).To(Succeed())

		Expect(stdout.String()).To(And(
			HavePrefix("::warning::"),
			ContainSubstring("exit status 1%0Acluster smoke: still running"),
		))
		Expect(strings.Count(stdout.String(), "\n")).To(Equal(1), "stdout holds %q", stdout.String())
	})

	DescribeTable("makes no call and exits 0 when --teardown is false, with or without --rooket",
		func(ctx SpecContext, flags ...string) {
			Expect(teardown(ctx, flags...)).To(Succeed())

			Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
			Expect(stdout.String()).To(BeEmpty())
		},
		Entry("with --rooket", "--rooket", rooketBin, "--teardown", "false"),
		Entry("without --rooket, in upper case", "--teardown", "FALSE"),
	)

	It("exits 0 on a second teardown in the same job", func(ctx SpecContext) {
		Expect(teardown(ctx, "--rooket", rooketBin)).To(Succeed())
		Expect(teardown(ctx, "--rooket", rooketBin)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(2), "Run calls")
		Expect(stdout.String()).To(BeEmpty())
	})

	It("fails on a --teardown that is neither true nor false, running nothing", func(ctx SpecContext) {
		err := teardown(ctx, "--rooket", rooketBin, "--teardown", "no")

		Expect(err).To(MatchError(`input teardown: "no" is not true or false`))
		Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
	})

	DescribeTable("prints one ::warning:: line, runs nothing, and exits 0 when teardown is enabled without a rooket binary",
		func(ctx SpecContext, flags ...string) {
			Expect(teardown(ctx, flags...)).To(Succeed())

			Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
			Expect(stdout.String()).To(And(HavePrefix("::warning::"), ContainSubstring("--rooket")))
			Expect(strings.Count(stdout.String(), "\n")).To(Equal(1), "stdout holds %q", stdout.String())
		},
		Entry("--rooket omitted"),
		Entry("--rooket empty, as the post step passes an unset state", "--rooket", "", "--teardown", "true"),
	)

	// setup's guard already keeps the post step off any other machine, by
	// writing no state; teardown checks again before it deletes anything.
	It("prints one ::warning:: naming the refusal, runs nothing, and exits 0 on a machine the guard refuses", func(ctx SpecContext) {
		env["RUNNER_ENVIRONMENT"] = "self-hosted"

		Expect(teardown(ctx, "--rooket", rooketBin)).To(Succeed())

		Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
		Expect(stdout.String()).To(And(
			HavePrefix("::warning::not tearing down rooket clusters: "),
			ContainSubstring(`RUNNER_ENVIRONMENT is "self-hosted"`),
		))
		Expect(strings.Count(stdout.String(), "\n")).To(Equal(1), "stdout holds %q", stdout.String())
	})

	It("consults the guard only for an enabled teardown, so --teardown false stays silent on a refused machine", func(ctx SpecContext) {
		delete(env, "GITHUB_ACTIONS")

		Expect(teardown(ctx, "--rooket", rooketBin, "--teardown", "false")).To(Succeed())

		Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
		Expect(stdout.String()).To(BeEmpty())
	})

	It("fails on an invalid --teardown before consulting the guard", func(ctx SpecContext) {
		delete(env, "GITHUB_ACTIONS")

		Expect(teardown(ctx, "--rooket", rooketBin, "--teardown", "no")).To(MatchError(`input teardown: "no" is not true or false`))

		Expect(runner.RunCallCount()).To(BeZero(), "Run calls")
		Expect(stdout.String()).To(BeEmpty())
	})

	It("builds its dependencies around the command's own output streams", func(ctx SpecContext) {
		var gotOut, gotErr io.Writer

		Expect(cli.RunWith(ctx, []string{"teardown", "--rooket", rooketBin}, stdout, stderr,
			func(o, e io.Writer) cli.Deps {
				gotOut, gotErr = o, e
				return d
			})).To(Succeed())

		Expect(gotOut).To(BeIdenticalTo(stdout))
		Expect(gotErr).To(BeIdenticalTo(stderr))
	})
})
