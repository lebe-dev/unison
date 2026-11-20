//go:build unix

package keystore

import (
	"os"
	"syscall"
)

// checkFile rejects a secret file that another user can read or that belongs to
// another user.
func checkFile(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(stat.Uid) != os.Geteuid() {
		return errForeignOwner
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errWidePermissions
	}
	return nil
}
