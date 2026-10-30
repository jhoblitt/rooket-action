package prepare_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/prepare"
	"github.com/jhoblitt/rooket-action/internal/prepare/preparefakes"
)

// calls returns each command r ran as its name followed by its args, and
// fails the spec if any ran with an env.
func calls(r *preparefakes.FakeRunner) [][]string {
	GinkgoHelper()
	got := make([][]string, 0, r.RunCallCount())
	for i := range r.RunCallCount() {
		_, env, name, args := r.RunArgsForCall(i)
		Expect(env).To(BeEmpty(), "env of call %d", i)
		got = append(got, append([]string{name}, args...))
	}
	return got
}

var _ = Describe("SetupISCSI", func() {
	var runner *preparefakes.FakeRunner

	BeforeEach(func() {
		runner = new(preparefakes.FakeRunner)
	})

	It("runs the four commands in order, each through sudo", func(ctx SpecContext) {
		Expect(prepare.SetupISCSI(ctx, runner)).To(Succeed())

		Expect(calls(runner)).To(Equal([][]string{
			{"sudo", "apt-get", "update"},
			{"sudo", "apt-get", "install", "-y", "open-iscsi", "targetcli-fb", "lvm2"},
			{"sudo", "modprobe", "-a", "configfs", "target_core_mod", "target_core_file", "iscsi_target_mod", "iscsi_tcp"},
			{"sudo", "systemctl", "enable", "--now", "iscsid"},
		}))
	})

	DescribeTable("stops at a failing command, naming its operation and keeping its error",
		func(ctx SpecContext, failing int, operation string) {
			cause := errors.New("exit status 1")
			runner.RunReturnsOnCall(failing, cause)

			err := prepare.SetupISCSI(ctx, runner)

			Expect(err).To(MatchError(ContainSubstring(operation)))
			Expect(err).To(MatchError(cause))
			Expect(runner.RunCallCount()).To(Equal(failing+1), "calls after command %d failed", failing)
		},
		Entry("apt-get update", 0, "updating the apt package lists"),
		Entry("apt-get install", 1, "installing open-iscsi, targetcli-fb and lvm2"),
		Entry("modprobe, never reaching systemctl", 2, "loading the iSCSI kernel modules"),
		Entry("systemctl", 3, "enabling and starting iscsid"),
	)
})
