// Package ghfiles appends to the files a GitHub Actions runner reads after a
// step: the ones GITHUB_PATH, GITHUB_ENV, GITHUB_OUTPUT and GITHUB_STATE
// name.
package ghfiles

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Set appends key=value to the GITHUB_ENV, GITHUB_OUTPUT or GITHUB_STATE
// file at path. A key or value holding a newline is an error, and nothing is
// written: the runner reads the file a line at a time, so a newline would
// end the value and start a line of the caller's choosing.
func Set(path, key, value string) error {
	if hasNewline(key) {
		return fmt.Errorf("key %q contains a newline", key)
	}
	if hasNewline(value) {
		return fmt.Errorf("value %q of %s contains a newline", value, key)
	}
	return appendLine(path, key+"="+value)
}

// AddPath appends dir to the GITHUB_PATH file at path, which puts dir at
// the front of PATH for the job's later steps. A dir holding a newline is an
// error, and nothing is written.
func AddPath(path, dir string) error {
	if hasNewline(dir) {
		return fmt.Errorf("directory %q contains a newline", dir)
	}
	return appendLine(path, dir)
}

func hasNewline(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}

// appendLine appends line and a newline to the file at path in one write,
// creating the file when it does not exist.
func appendLine(path, line string) (err error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // G304: path is the runner's command file, named by the runner's environment
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	_, err = f.WriteString(line + "\n")
	return err
}
