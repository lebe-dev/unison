//go:build darwin || windows

package download

import (
	"os"
	"path/filepath"
)

// downloadBase returns the download folder of the user.
func downloadBase() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}
