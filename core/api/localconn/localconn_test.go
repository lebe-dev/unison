//go:build !windows

package localconn_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/api/localconn"
)

// testTimeout bounds the waits of the tests.
const testTimeout = 10 * time.Second

// shortDir returns a temporary directory with a short path, so a socket path
// stays below the limit of the platform.
func shortDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "unison")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// serve starts the endpoint and serves handler on it.
func serve(t *testing.T, path string, handler http.Handler) {
	t.Helper()

	listener, err := localconn.Listen(path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- localconn.Serve(ctx, listener, handler) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(testTimeout):
			t.Error("serve did not return")
		}
	})
}

// TestServeOverHTTP2 checks that the endpoint speaks HTTP/2 and answers a
// request from the client.
func TestServeOverHTTP2(t *testing.T) {
	path := filepath.Join(shortDir(t), "unison.sock")
	serve(t, path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("request proto is %s, want HTTP/2", r.Proto)
		}
		_, _ = io.WriteString(w, "ok")
	}))

	client := localconn.NewHTTPClient(path)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, localconn.BaseURL+"/ping", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body is %q, want %q", body, "ok")
	}
}

// TestListenPermissions checks that the directory and the socket are private to
// the user.
func TestListenPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "unison")
	path := filepath.Join(dir, "unison.sock")
	serve(t, path, http.NotFoundHandler())

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("directory mode is %o, want 700", perm)
	}

	socketInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if perm := socketInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode is %o, want 600", perm)
	}
}

// TestDialRefusesOpenSocket checks that a socket other users can reach is
// refused before it is used.
func TestDialRefusesOpenSocket(t *testing.T) {
	path := filepath.Join(shortDir(t), "shared.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := localconn.Dial(ctx, path)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("dial of a socket other users can reach succeeded")
	}
	if !errors.Is(err, localconn.ErrInsecureEndpoint) {
		t.Fatalf("dial error is %v, want %v", err, localconn.ErrInsecureEndpoint)
	}
}

// TestDialRefusesFile checks that a regular file is not taken for an endpoint.
func TestDialRefusesFile(t *testing.T) {
	path := filepath.Join(shortDir(t), "unison.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := localconn.Dial(ctx, path); !errors.Is(err, localconn.ErrNotSocket) {
		t.Fatalf("dial error is %v, want %v", err, localconn.ErrNotSocket)
	}
}

// TestDialRefusesSharedDirectory checks that an endpoint another user can
// replace is refused.
func TestDialRefusesSharedDirectory(t *testing.T) {
	dir := filepath.Join(shortDir(t), "shared")
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	path := filepath.Join(dir, "unison.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := localconn.Dial(ctx, path); !errors.Is(err, localconn.ErrInsecureEndpoint) {
		t.Fatalf("dial error is %v, want %v", err, localconn.ErrInsecureEndpoint)
	}
}

// TestDefaultPathOverride checks that the environment overrides the endpoint.
func TestDefaultPathOverride(t *testing.T) {
	path := filepath.Join(shortDir(t), "other.sock")
	t.Setenv(localconn.EnvPath, path)

	got, err := localconn.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if got != path {
		t.Fatalf("default path is %q, want %q", got, path)
	}
}
