//go:build !windows

package localconn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// Socket permissions. The directory is private to the user and the socket is
// readable and writable by the owner only.
const (
	dirMode    os.FileMode = 0o700
	socketMode os.FileMode = 0o600
)

// platformPath returns the endpoint of the daemon on this platform.
func platformPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("localconn: locate the home directory: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Unison", "unisond.sock"), nil
	}
	return filepath.Join(home, ".local", "share", "unison", "unisond.sock"), nil
}

// Listen binds the socket of the endpoint. The directory is created private to
// the user and the socket is bound with owner only access. An endpoint that is
// already bound is an error, so the caller removes a stale socket.
func Listen(path string) (net.Listener, error) {
	if path == "" {
		return nil, errors.New("localconn: endpoint path is required")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("localconn: create the endpoint directory: %w", err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return nil, fmt.Errorf("localconn: secure the endpoint directory: %w", err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, socketMode); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("localconn: secure the endpoint: %w", err)
	}
	if err := checkEndpoint(path); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

// Dial connects to the socket of the endpoint. A socket that another user owns
// or that other users can reach is refused before it is used.
func Dial(ctx context.Context, path string) (net.Conn, error) {
	if path == "" {
		return nil, errors.New("localconn: endpoint path is required")
	}
	if err := checkEndpoint(path); err != nil {
		return nil, err
	}

	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", path)
}

// checkEndpoint verifies that the endpoint is a socket of the current user that
// only the owner can reach, and that other users cannot replace it.
func checkEndpoint(path string) error {
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("localconn: read the endpoint directory: %w", err)
	}
	if dirInfo.Mode().Perm()&0o022 != 0 {
		return ErrInsecureEndpoint
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("localconn: read the endpoint: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return ErrNotSocket
	}
	if info.Mode().Perm()&0o077 != 0 {
		return ErrInsecureEndpoint
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("localconn: read the endpoint owner")
	}
	if int(stat.Uid) != os.Getuid() {
		return ErrInsecureEndpoint
	}
	return nil
}
