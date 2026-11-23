//go:build darwin

package localconn_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lebe-dev/unison/core/api/localconn"
)

// TestDefaultPathPlatform checks the endpoint of the platform.
func TestDefaultPathPlatform(t *testing.T) {
	t.Setenv(localconn.EnvPath, "")

	path, err := localconn.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home directory: %v", err)
	}
	if want := filepath.Join(home, "Library", "Application Support", "Unison", "unisond.sock"); path != want {
		t.Fatalf("default path is %q, want %q", path, want)
	}
}
