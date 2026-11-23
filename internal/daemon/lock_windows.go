//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// lockRange is the number of bytes the lock covers.
const lockRange = 1

// tryLock takes an exclusive lock on the file without waiting.
func tryLock(file *os.File) error {
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, lockRange, 0, overlapped)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return ErrLocked
	default:
		return fmt.Errorf("daemon: lock the lock file: %w", err)
	}
}

// unlockFile releases the lock of the file.
func unlockFile(file *os.File) error {
	err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, lockRange, 0, new(windows.Overlapped))
	if err != nil {
		return fmt.Errorf("daemon: unlock the lock file: %w", err)
	}
	return nil
}
