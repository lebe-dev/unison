package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/internal/xdg"
)

// Names of the files and directories the daemon owns inside a data directory.
const (
	// LockFileName is the single instance lock of the daemon.
	LockFileName = "unisond.lock"
	// SocketFileName is the endpoint of the local API on platforms with unix
	// sockets.
	SocketFileName = "unisond.sock"
	// LogsDirName holds the log files of the daemon.
	LogsDirName = "logs"
	// LogFileName is the current log file.
	LogFileName = "unisond.log"
	// KeysDirName holds the keystore files on Windows.
	KeysDirName = "keys"
)

// ResolveDataDir returns the data directory: dir when it is set, the per user
// directory of the platform otherwise.
func ResolveDataDir(dir string) (string, error) {
	if dir == "" {
		return DefaultDataDir()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("daemon: resolve the data directory: %w", err)
	}
	return filepath.Clean(abs), nil
}

// DefaultDataDir returns the data directory of the current user.
func DefaultDataDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Unison"), nil
	case "windows":
		root := os.Getenv("LOCALAPPDATA")
		if root == "" {
			return "", errors.New("daemon: LOCALAPPDATA is not set")
		}
		return filepath.Join(root, "Unison"), nil
	case "linux":
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		return linuxDataDir(home, os.Getenv("XDG_DATA_HOME")), nil
	default:
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "unison"), nil
	}
}

// linuxDataDir returns the data directory on Linux: the XDG data home when the
// environment names an absolute one, the default data home otherwise.
func linuxDataDir(home, xdgDataHome string) string {
	return filepath.Join(xdg.DataHome(home, xdgDataHome), "unison")
}

// homeDir returns the home directory of the current user.
func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("daemon: locate the home directory: %w", err)
	}
	return home, nil
}

// SocketPath returns the local API endpoint of a data directory. The
// UNISON_LOCAL_API environment variable overrides it for every data directory.
func SocketPath(dataDir string) (string, error) {
	if path := os.Getenv(localconn.EnvPath); path != "" {
		return path, nil
	}
	if dataDir == "" {
		def, err := DefaultDataDir()
		if err != nil {
			return "", err
		}
		dataDir = def
	}
	dataDir = filepath.Clean(dataDir)
	if def, err := DefaultDataDir(); err == nil && samePath(dataDir, def) {
		return localconn.DefaultPath()
	}
	// A named pipe has no file system to put the endpoint into, so a data
	// directory of its own gets a pipe name derived from its path.
	if runtime.GOOS == "windows" {
		return pipePath(dataDir), nil
	}
	return filepath.Join(dataDir, SocketFileName), nil
}

// LogFile returns the current log file of a data directory.
func LogFile(dataDir string) string {
	return filepath.Join(dataDir, LogsDirName, LogFileName)
}

// LockFile returns the single instance lock of a data directory.
func LockFile(dataDir string) string {
	return filepath.Join(dataDir, LockFileName)
}

// pipePath returns the named pipe of a data directory other than the default.
func pipePath(dataDir string) string {
	return `\\.\pipe\unisond-` + shortHash(dataDir)
}

// samePath reports whether two paths name the same directory. Windows compares
// paths without the case, so another spelling of the default data directory
// keeps the endpoint of the platform.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// shortHash returns four bytes of the hash of a path, which tells the data
// directories of two daemons apart.
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(value)))
	return hex.EncodeToString(sum[:4])
}
