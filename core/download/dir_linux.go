//go:build linux

package download

import (
	"os"
	"path/filepath"

	"github.com/lebe-dev/unison/internal/xdg"
)

// downloadBase returns the download folder of the desktop session, or Downloads
// below the home directory when the session did not set one.
func downloadBase() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if dir := userDirsDownloadDir(home); dir != "" {
		return dir
	}
	return filepath.Join(home, "Downloads")
}

// userDirsDownloadDir returns the download folder of the user-dirs.dirs file of
// the session.
func userDirsDownloadDir(home string) string {
	path := filepath.Join(xdg.ConfigHome(home, os.Getenv("XDG_CONFIG_HOME")), "user-dirs.dirs")
	data, err := os.ReadFile(path) //nolint:gosec // The path is the user-dirs.dirs file of the session.
	if err != nil {
		return ""
	}
	return parseUserDirs(string(data), home)
}
