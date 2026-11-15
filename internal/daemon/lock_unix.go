//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes an exclusive advisory lock on the file without waiting.
func tryLock(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EWOULDBLOCK):
		return ErrLocked
	default:
		return fmt.Errorf("daemon: lock the lock file: %w", err)
	}
}

// unlockFile releases the advisory lock of the file.
func unlockFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("daemon: unlock the lock file: %w", err)
	}
	return nil
}
