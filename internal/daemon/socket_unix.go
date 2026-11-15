//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// removeStaleSocket deletes an endpoint that no instance listens on. The
// caller holds the lock file, so no other instance is starting up.
func removeStaleSocket(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("daemon: remove the stale endpoint: %w", err)
	}
	return nil
}
