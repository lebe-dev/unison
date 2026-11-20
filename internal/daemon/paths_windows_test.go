//go:build windows

package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/api/localconn"
)

// switchCase changes the case of every ASCII letter, so the result names the
// same path on Windows and stays a valid path on any file system.
func switchCase(path string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case 'a' <= r && r <= 'z':
			return r - 'a' + 'A'
		case 'A' <= r && r <= 'Z':
			return r - 'A' + 'a'
		default:
			return r
		}
	}, path)
}

// TestSamePathIgnoresCase checks that two spellings of one directory are one
// path on Windows.
func TestSamePathIgnoresCase(t *testing.T) {
	if !samePath(`C:\ProgramData\Unison`, `c:\programdata\UNISON`) {
		t.Fatal("samePath took two spellings of one directory for two")
	}
	if samePath(`C:\ProgramData\Unison`, `C:\ProgramData\Unison2`) {
		t.Fatal("samePath took two directories for one")
	}
	if !samePath(`C:\ProgramData\Unison\`, `C:\ProgramData\Unison`) {
		t.Fatal("samePath kept a trailing separator")
	}
}

// TestSocketPathDefaultIgnoresCase checks that the default data directory keeps
// the endpoint of the platform when it is written in another case, which is how
// the app finds the daemon.
func TestSocketPathDefaultIgnoresCase(t *testing.T) {
	t.Setenv(localconn.EnvPath, "")
	dir, err := DefaultDataDir()
	if err != nil {
		t.Skipf("the default data directory is unknown: %v", err)
	}
	want, err := localconn.DefaultPath()
	if err != nil {
		t.Fatalf("default endpoint: %v", err)
	}

	for _, spelled := range []string{dir, switchCase(dir)} {
		got, err := SocketPath(spelled)
		if err != nil {
			t.Fatalf("socket path of %q: %v", spelled, err)
		}
		if got != want {
			t.Errorf("socket path of %q is %q, want %q", spelled, got, want)
		}
	}
}

// TestSocketPathOwnDirectory checks that a data directory of its own gets a
// stable named pipe of its own.
func TestSocketPathOwnDirectory(t *testing.T) {
	t.Setenv(localconn.EnvPath, "")
	dir := filepath.Join(t.TempDir(), "data")

	path, err := SocketPath(dir)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if !strings.HasPrefix(path, `\\.\pipe\unisond-`) {
		t.Fatalf("socket path is %q, want a pipe below \\\\.\\pipe\\", path)
	}

	again, err := SocketPath(dir)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if again != path {
		t.Errorf("the pipe of %q changed from %q to %q", dir, path, again)
	}

	other, err := SocketPath(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if other == path {
		t.Errorf("two data directories share the pipe %q", path)
	}
}

// TestSocketPathOverride checks that the environment overrides the endpoint of
// every data directory.
func TestSocketPathOverride(t *testing.T) {
	const override = `\\.\pipe\unison-test`
	t.Setenv(localconn.EnvPath, override)

	dir := filepath.Join(t.TempDir(), "data")
	path, err := SocketPath(dir)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if path != override {
		t.Errorf("socket path is %q, want %q", path, override)
	}
}
