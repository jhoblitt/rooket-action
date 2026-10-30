package prepare

import (
	"context"
	"fmt"
)

// iscsiCommands are what SetupISCSI runs through sudo, in order, each with
// the operation an error names in words: sys.Exec's error already names the
// command line.
var iscsiCommands = []struct {
	operation string
	args      []string
}{
	{"updating the apt package lists", []string{"apt-get", "update"}},
	{"installing open-iscsi, targetcli-fb and lvm2", []string{"apt-get", "install", "-y", "open-iscsi", "targetcli-fb", "lvm2"}},
	{"loading the iSCSI kernel modules", []string{"modprobe", "-a", "configfs", "target_core_mod", "target_core_file", "iscsi_target_mod", "iscsi_tcp"}},
	{"enabling and starting iscsid", []string{"systemctl", "enable", "--now", "iscsid"}},
}

// SetupISCSI installs the iSCSI and LVM tooling rooket gives kind nodes block
// devices with, loads the kernel modules it needs, and enables and starts
// iscsid, each through sudo on r. It stops at the first command that fails.
// It creates no targets and no disk images; those are rooket's.
//
// The modules are loaded by name, so a runner image that lacks one fails
// here rather than somewhere inside rooket.
func SetupISCSI(ctx context.Context, r Runner) error {
	for _, c := range iscsiCommands {
		if err := r.Run(ctx, nil, "sudo", c.args...); err != nil {
			return fmt.Errorf("%s: %w", c.operation, err)
		}
	}
	return nil
}
