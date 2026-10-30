//go:build selftest

// Package selftest_test checks the runner the action left behind. It runs
// in self-test.yml, in a later step of the job that ran the action, and
// sits behind the selftest build tag because it reads that runner rather
// than a fixture.
package selftest_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/tools"
)

// versionArgs makes each pinned tool print its version.
var versionArgs = map[string][]string{
	"kind":    {"version"},
	"kubectl": {"version", "--client"},
	"helm":    {"version"},
}

// pinEntries is one table entry for each tool the helper pins, so that a
// new pin cannot go unchecked.
func pinEntries() []TableEntry {
	pins := tools.Pins()
	entries := make([]TableEntry, 0, len(pins))
	for _, pin := range pins {
		entries = append(entries, Entry(pin.Name, pin))
	}
	return entries
}

// output returns what name prints, and fails the spec with that output
// when name exits non-zero.
func output(ctx context.Context, name string, args ...string) string {
	GinkgoHelper()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "%s %s:\n%s", name, strings.Join(args, " "), out)
	return string(out)
}

// fields returns each line's whitespace-separated fields.
func fields(s string) [][]string {
	var out [][]string
	for line := range strings.Lines(s) {
		out = append(out, strings.Fields(line))
	}
	return out
}

var _ = Describe("the action's binaries", func() {
	var rooketPath string

	BeforeEach(func() {
		rooketPath = os.Getenv("ROOKET_ACTION_PATH")
		Expect(rooketPath).NotTo(BeEmpty(), "ROOKET_ACTION_PATH must hold the action's path output")
	})

	It("puts the action's rooket first on PATH", func() {
		got, err := exec.LookPath("rooket")

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(rooketPath), "rooket on PATH")
	})

	It("installs the pinned rooket release", func(ctx SpecContext) {
		Expect(output(ctx, rooketPath, "version")).To(ContainSubstring(tools.DefaultRooket().Version))
	})

	DescribeTable("installs the pinned tool beside rooket",
		func(ctx SpecContext, pin tools.Pin) {
			Expect(versionArgs).To(HaveKey(pin.Name), "the self-test has no version command for pinned tool %q", pin.Name)

			got, err := exec.LookPath(pin.Name)

			Expect(err).NotTo(HaveOccurred())
			Expect(filepath.Dir(got)).To(Equal(filepath.Dir(rooketPath)), "directory of %s on PATH", pin.Name)
			Expect(output(ctx, got, versionArgs[pin.Name]...)).To(ContainSubstring(pin.Version), "%s's version", pin.Name)
		},
		pinEntries(),
	)
})

var _ = Describe("the runner's preparation", func() {
	It("selects docker as rooket's engine", func() {
		Expect(os.Getenv("ROOKET_ENGINE")).To(Equal("docker"), "ROOKET_ENGINE")
	})

	It("runs iscsid", func(ctx SpecContext) {
		// is-active exits non-zero for any state but active, and what it
		// prints is the useful failure.
		out, err := exec.CommandContext(ctx, "systemctl", "is-active", "iscsid").CombinedOutput()

		Expect(strings.TrimSpace(string(out))).To(Equal("active"), "systemctl is-active iscsid, which returned %v", err)
	})

	It("loads the iSCSI target and initiator modules", func(ctx SpecContext) {
		var loaded []string
		for _, f := range fields(output(ctx, "lsmod")) {
			if len(f) > 0 {
				loaded = append(loaded, f[0])
			}
		}

		Expect(loaded).To(ContainElements("target_core_mod", "target_core_file", "iscsi_target_mod", "iscsi_tcp"))
	})

	It("mounts configfs at /sys/kernel/config", func() {
		data, err := os.ReadFile("/proc/mounts")
		Expect(err).NotTo(HaveOccurred())

		fsType := map[string]string{}
		for _, f := range fields(string(data)) {
			if len(f) >= 3 {
				fsType[f[1]] = f[2]
			}
		}

		Expect(fsType).To(HaveKeyWithValue("/sys/kernel/config", "configfs"))
	})
})
