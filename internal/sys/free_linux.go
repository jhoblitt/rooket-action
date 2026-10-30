package sys

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Free returns the bytes available to an unprivileged user on the
// filesystem holding path.
func Free(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	if st.Bsize <= 0 {
		return 0, fmt.Errorf("statfs %s: block size %d", path, st.Bsize)
	}
	return st.Bavail * uint64(st.Bsize), nil
}
