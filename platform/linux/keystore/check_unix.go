//go:build unix

package keystore

import (
	"errors"
	"os"
	"syscall"
)

// errForeignOwner reports a secret file of another user.
var errForeignOwner = errors.New("keystore: the secret file belongs to another user")

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
