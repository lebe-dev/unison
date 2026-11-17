package daemon

import (
	"errors"
	"fmt"
	"os"
)

// ErrLocked reports that another process holds the lock file.
var ErrLocked = errors.New("daemon: the lock file is held by another process")

// lock is the single instance lock of a data directory. The operating system
// releases it when the process ends, so a crash leaves no lock behind.
type lock struct {
	file *os.File
}

// acquireLock takes the exclusive lock of path without waiting.
func acquireLock(path string) (*lock, error) {
	//nolint:gosec // The lock file lives in the data directory of the daemon.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: open the lock file: %w", err)
	}
	if err := tryLock(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &lock{file: file}, nil
}

// release unlocks and closes the lock file. The file stays on disk, so the next
// process takes it over instead of creating a new one.
func (l *lock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return errors.Join(unlockFile(file), file.Close())
}
