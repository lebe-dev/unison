package daemon_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/internal/daemon"
)

// TestDefaultDataDir checks that every supported platform has a data
// directory.
func TestDefaultDataDir(t *testing.T) {
	dir, err := daemon.DefaultDataDir()
	if err != nil {
		t.Fatalf("default data dir: %v", err)
	}
	if dir == "" || !filepath.IsAbs(dir) {
		t.Errorf("data dir is %q, want an absolute path", dir)
	}
}

// TestResolveDataDir checks that a relative directory is made absolute.
func TestResolveDataDir(t *testing.T) {
	dir, err := daemon.ResolveDataDir("data")
	if err != nil {
		t.Fatalf("resolve data dir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("data dir is %q, want an absolute path", dir)
	}
	if filepath.Base(dir) != "data" {
		t.Errorf("data dir is %q, want a directory named data", dir)
	}
}

// TestSocketPath checks that the endpoint follows the data directory and the
// override of the environment.
func TestSocketPath(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		// A named pipe cannot live in a directory.
		t.Skip("the endpoint is a named pipe on Windows")
	}

	path, err := daemon.SocketPath(dir)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if want := filepath.Join(dir, daemon.SocketFileName); path != want {
		t.Errorf("socket path is %q, want %q", path, want)
	}

	override := filepath.Join(dir, "other.sock")
	t.Setenv(localconn.EnvPath, override)
	path, err = daemon.SocketPath(dir)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if path != override {
		t.Errorf("socket path is %q, want %q", path, override)
	}
}

// TestSocketPathDefault checks that the default data directory uses the
// endpoint of the platform.
func TestSocketPathDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the endpoint is a named pipe on Windows")
	}
	t.Setenv(localconn.EnvPath, "")

	dir, err := daemon.DefaultDataDir()
	if err != nil {
		t.Fatalf("default data dir: %v", err)
	}
	path, err := daemon.SocketPath(dir)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	want, err := localconn.DefaultPath()
	if err != nil {
		t.Fatalf("default endpoint: %v", err)
	}
	if path != want {
		t.Errorf("socket path is %q, want %q", path, want)
	}
}
