//go:build linux

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lebe-dev/unison/core/api/localconn"
)

// TestLockFilePermissions checks that the lock file is private to the user.
func TestLockFilePermissions(t *testing.T) {
	dir := t.TempDir()
	instance, err := acquireLock(LockFile(dir))
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer func() { _ = instance.release() }()

	info, err := os.Stat(LockFile(dir))
	if err != nil {
		t.Fatalf("read the lock file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("lock file has mode %o, want 600", perm)
	}
}

// TestSocketPermissions checks the endpoint of a data directory of its own on
// Linux: a private directory, a socket of the owner and a stale socket that is
// removed at startup.
func TestSocketPermissions(t *testing.T) {
	dir := t.TempDir()
	endpoint, err := SocketPath(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if filepath.Base(endpoint) != SocketFileName {
		t.Fatalf("endpoint is %s, want a name of %s", endpoint, SocketFileName)
	}
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		t.Fatalf("create the data directory: %v", err)
	}

	listener, err := localconn.Listen(endpoint)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	dirInfo, err := os.Stat(filepath.Dir(endpoint))
	if err != nil {
		t.Fatalf("read the data directory: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("data directory has mode %o, want 700", perm)
	}
	socketInfo, err := os.Lstat(endpoint)
	if err != nil {
		t.Fatalf("read the endpoint: %v", err)
	}
	if perm := socketInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("endpoint has mode %o, want 600", perm)
	}
	_ = listener.Close()

	// A crash leaves the endpoint behind, so the next start removes it.
	if err := os.WriteFile(endpoint, nil, 0o600); err != nil {
		t.Fatalf("write the stale endpoint: %v", err)
	}
	if err := removeStaleSocket(endpoint); err != nil {
		t.Fatalf("remove the stale endpoint: %v", err)
	}
	if _, err := os.Lstat(endpoint); !os.IsNotExist(err) {
		t.Fatalf("the stale endpoint is still there: %v", err)
	}
	if err := removeStaleSocket(endpoint); err != nil {
		t.Fatalf("remove a missing endpoint: %v", err)
	}
}

// TestSocketPathOverride checks that the environment names the endpoint.
func TestSocketPathOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.sock")
	t.Setenv(localconn.EnvPath, path)
	got, err := SocketPath(t.TempDir())
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	if got != path {
		t.Fatalf("endpoint is %q, want %q", got, path)
	}
}

// TestEndpointAlive checks the check of the endpoint before the daemon binds
// it.
func TestEndpointAlive(t *testing.T) {
	dir := t.TempDir()
	endpoint, err := SocketPath(dir)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	ctx := context.Background()
	if endpointAlive(ctx, endpoint) {
		t.Fatal("an endpoint without a listener reports a live instance")
	}

	listener, err := localconn.Listen(endpoint)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	if !endpointAlive(ctx, endpoint) {
		t.Fatal("a listening endpoint reports no instance")
	}
}
