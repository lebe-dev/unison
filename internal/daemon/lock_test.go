package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestLockSingleHolder checks that one lock file is held by one process at a
// time and can be taken again after it was released.
func TestLockSingleHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), LockFileName)

	first, err := acquireLock(path)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if _, err := acquireLock(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock returned %v, want ErrLocked", err)
	}
	if err := first.release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	second, err := acquireLock(path)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	if err := second.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file is missing: %v", err)
	}
}
