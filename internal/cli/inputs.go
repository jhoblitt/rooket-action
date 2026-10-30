package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// The action's inputs reach the helper as strings, which these parse. Each
// trims surrounding whitespace first, as the Actions toolkit's getInput
// does, and an error names the input and quotes the value as given.

// parseBool reads the input name as true or false, in any case.
func parseBool(name, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("input %s: %q is not true or false", name, value)
}

// parseCount reads the input name as a whole number of at least 1.
func parseCount(name, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("input %s: %q is not a whole number of at least 1", name, value)
	}
	return n, nil
}

// parseOptionalCount reads the input name as parseCount does, except that
// an empty one reads as 0.
func parseOptionalCount(name, value string) (int, error) {
	s := strings.TrimSpace(value)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("input %s: %q is neither empty nor a whole number of at least 1", name, value)
	}
	return n, nil
}
