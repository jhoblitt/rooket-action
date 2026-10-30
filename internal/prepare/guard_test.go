package prepare_test

import (
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/prepare"
)

// hosted is the environment of a GitHub-hosted Linux X64 runner.
func hosted() map[string]string {
	return map[string]string{
		"GITHUB_ACTIONS":     "true",
		"RUNNER_ENVIRONMENT": "github-hosted",
		"RUNNER_OS":          "Linux",
		"RUNNER_ARCH":        "X64",
	}
}

// getenv reads env as os.Getenv reads the process environment: a variable
// that is not set reads as "".
func getenv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

var _ = Describe("Guard", func() {
	It("passes a GitHub-hosted Linux X64 runner", func() {
		Expect(prepare.Guard(getenv(hosted()))).To(Succeed())
	})

	DescribeTable("refuses any other runner, naming the variable and its value",
		func(name string, value *string) {
			env := hosted()
			delete(env, name)
			if value != nil {
				env[name] = *value
			}

			Expect(prepare.Guard(getenv(env))).To(MatchError(And(
				ContainSubstring(name),
				ContainSubstring(strconv.Quote(env[name])),
			)))
		},
		Entry("outside GitHub Actions", "GITHUB_ACTIONS", new("false")),
		Entry("a self-hosted runner", "RUNNER_ENVIRONMENT", new("self-hosted")),
		Entry("a macOS runner", "RUNNER_OS", new("macOS")),
		Entry("an ARM64 runner", "RUNNER_ARCH", new("ARM64")),
		Entry("RUNNER_ENVIRONMENT unset", "RUNNER_ENVIRONMENT", nil),
	)
})
