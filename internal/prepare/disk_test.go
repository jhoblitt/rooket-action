package prepare_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/prepare"
	"github.com/jhoblitt/rooket-action/internal/prepare/preparefakes"
	"github.com/jhoblitt/rooket-action/internal/sys"
)

// sys.Exec is the Runner production hands prepare.
var _ prepare.Runner = sys.Exec{}

const gib = uint64(1) << 30

// disk scripts what free reports: each reading in turn, then err.
type disk struct {
	readings []uint64
	err      error
	paths    []string
}

func (d *disk) free(path string) (uint64, error) {
	d.paths = append(d.paths, path)
	if len(d.readings) == 0 {
		Expect(d.err).To(HaveOccurred(), "free(%q) called more often than scripted", path)
		return 0, d.err
	}
	got := d.readings[0]
	d.readings = d.readings[1:]
	return got, nil
}

var _ = Describe("Threshold", func() {
	DescribeTable("returns the bytes that must be free",
		func(workers, minFreeGiB int, wantGiB uint64) {
			Expect(prepare.Threshold(workers, minFreeGiB)).To(Equal(wantGiB * gib))
		},
		Entry("one worker by the formula", 1, 0, uint64(40)),
		Entry("three workers by the formula", 3, 0, uint64(60)),
		Entry("min-free-gb overriding the formula", 3, 25, uint64(25)),
		Entry("a negative min-free-gb meaning the formula", 1, -5, uint64(40)),
		Entry("workers below one counting as one", 0, 0, uint64(40)),
	)
})

var _ = Describe("EnsureDisk", func() {
	var (
		runner *preparefakes.FakeRunner
		warn   *bytes.Buffer
	)

	BeforeEach(func() {
		runner = new(preparefakes.FakeRunner)
		warn = new(bytes.Buffer)

		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(GinkgoWriter, nil)))
		DeferCleanup(slog.SetDefault, prev)
	})

	DescribeTable("leaves the disk alone when enough is free",
		func(ctx SpecContext, free uint64) {
			d := &disk{readings: []uint64{free}}

			Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(Succeed())

			Expect(runner.RunCallCount()).To(Equal(0))
			Expect(d.paths).To(Equal([]string{"/"}))
			Expect(warn.String()).To(BeEmpty())
		},
		Entry("more than needed", 50*gib),
		Entry("exactly what is needed", 40*gib),
	)

	It("when short, deletes the preinstalled toolchains in one sudo rm -rf", func(ctx SpecContext) {
		d := &disk{readings: []uint64{10 * gib, 50 * gib}}

		Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(1))
		_, env, name, args := runner.RunArgsForCall(0)
		Expect(env).To(BeEmpty())
		Expect(name).To(Equal("sudo"))
		Expect(args).To(Equal([]string{
			"rm", "-rf",
			"/usr/share/dotnet",
			"/usr/local/lib/android",
			"/opt/ghc",
			"/opt/hostedtoolcache/CodeQL",
			"/usr/local/.ghcup",
			"/usr/lib/jvm",
			"/usr/share/swift",
			"/usr/local/share/powershell",
		}))
		Expect(d.paths).To(Equal([]string{"/", "/"}))
		Expect(warn.String()).To(BeEmpty())
	})

	It("warns with the free and needed GiB when still short after cleanup, and succeeds", func(ctx SpecContext) {
		d := &disk{readings: []uint64{10 * gib, 25*gib + gib/2}}

		Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(1))
		Expect(warn.String()).To(Equal(
			"::warning::25.5 GiB free on / after deleting preinstalled toolchains; 40.0 GiB needed\n"))
	})

	It("warns and deletes nothing when the first measurement fails", func(ctx SpecContext) {
		d := &disk{err: errors.New("statfs failed")}

		Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(0))
		Expect(warn.String()).To(And(HavePrefix("::warning::"), ContainSubstring("statfs failed")))
		Expect(strings.Count(warn.String(), "\n")).To(Equal(1), "warn holds %q", warn.String())
	})

	It("warns when the deletion fails, and measures again", func(ctx SpecContext) {
		runner.RunReturnsOnCall(0, errors.New("rm failed"))
		d := &disk{readings: []uint64{10 * gib, 50 * gib}}

		Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(1))
		Expect(d.paths).To(Equal([]string{"/", "/"}))
		Expect(warn.String()).To(And(HavePrefix("::warning::"), ContainSubstring("rm failed")))
		Expect(strings.Count(warn.String(), "\n")).To(Equal(1), "warn holds %q", warn.String())
	})

	It("warns when the measurement after the deletion fails", func(ctx SpecContext) {
		d := &disk{readings: []uint64{10 * gib}, err: errors.New("statfs failed")}

		Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(Succeed())

		Expect(runner.RunCallCount()).To(Equal(1))
		Expect(warn.String()).To(And(HavePrefix("::warning::"), ContainSubstring("statfs failed")))
		Expect(strings.Count(warn.String(), "\n")).To(Equal(1), "warn holds %q", warn.String())
	})

	It("escapes an error's newline and percent sign so the warning stays one workflow command", func(ctx SpecContext) {
		d := &disk{err: errors.New("100% broken\nsecond line")}

		Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(Succeed())

		Expect(warn.String()).To(Equal(
			"::warning::measuring free disk space on /: 100%25 broken%0Asecond line; deleting nothing\n"))
	})

	DescribeTable("returns the context's error in place of a warning once canceled",
		func(sctx SpecContext, readings []uint64, freeErr, runErr error) {
			ctx, cancel := context.WithCancel(sctx)
			cancel()
			runner.RunReturns(runErr)
			d := &disk{readings: readings, err: freeErr}

			Expect(prepare.EnsureDisk(ctx, runner, d.free, 40*gib, warn)).To(MatchError(context.Canceled))

			Expect(warn.String()).To(BeEmpty())
		},
		Entry("at a failed first measurement", nil, errors.New("statfs failed"), nil),
		Entry("at a failed deletion", []uint64{10 * gib}, nil, errors.New("signal: killed")),
		Entry("at a failed second measurement", []uint64{10 * gib}, errors.New("statfs failed"), nil),
		Entry("when still short", []uint64{10 * gib, 20 * gib}, nil, nil),
	)
})
