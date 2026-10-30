package prepare

import "fmt"

// hostedRunner is the environment GitHub sets on a GitHub-hosted Linux X64
// runner, in the order Guard checks it.
var hostedRunner = []struct{ name, want string }{
	{"GITHUB_ACTIONS", "true"},
	{"RUNNER_ENVIRONMENT", "github-hosted"},
	{"RUNNER_OS", "Linux"},
	{"RUNNER_ARCH", "X64"},
}

// Guard refuses any machine but a disposable GitHub-hosted Linux X64 runner,
// reading the environment through getenv. Its error names the first
// variable that does not hold its required value, and the value it holds.
//
// Guard is what makes EnsureDisk's deletions safe, so it runs before
// anything else touches the machine.
func Guard(getenv func(string) string) error {
	for _, v := range hostedRunner {
		if got := getenv(v.name); got != v.want {
			return fmt.Errorf("refusing to prepare this machine: %s is %q, want %q; rooket-action deletes files and runs only on a GitHub-hosted Linux X64 runner",
				v.name, got, v.want)
		}
	}
	return nil
}
