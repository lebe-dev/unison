//go:build linux

package localconn_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lebe-dev/unison/core/api/localconn"
)

// TestDefaultPathPlatform checks the endpoint of the data home of the session.
func TestDefaultPathPlatform(t *testing.T) {
	t.Setenv(localconn.EnvPath, "")
	t.Setenv("XDG_DATA_HOME", "")

	path, err := localconn.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home directory: %v", err)
	}
	if want := filepath.Join(home, ".local", "share", "unison", "unisond.sock"); path != want {
		t.Fatalf("default path is %q, want %q", path, want)
	}
}

// TestDefaultPathDataHome checks that XDG_DATA_HOME moves the endpoint.
func TestDefaultPathDataHome(t *testing.T) {
	t.Setenv(localconn.EnvPath, "")
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	path, err := localconn.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if want := filepath.Join(data, "unison", "unisond.sock"); path != want {
		t.Fatalf("default path is %q, want %q", path, want)
	}
}
