// Package ghcmd writes GitHub Actions workflow commands, the ::name:: lines
// a runner reads from a step's stdout.
package ghcmd

import (
	"fmt"
	"io"
	"strings"
)

// data escapes what would otherwise end or garble a workflow command's
// message, as the Actions runner expects.
var data = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")

// Warning writes msg to w as one ::warning:: workflow command, escaped so
// that a newline in msg cannot end the command early.
func Warning(w io.Writer, msg string) {
	fmt.Fprintf(w, "::warning::%s\n", data.Replace(msg))
}
