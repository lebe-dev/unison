// Package xdg resolves the directories of the XDG base directory
// specification.
package xdg

import "path/filepath"

// DataHome returns the data directory of the user: dir when it is an absolute
// path, home/.local/share otherwise. Empty and relative values are ignored, as
// the specification demands.
func DataHome(home, dir string) string {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(home, ".local", "share")
}

// ConfigHome returns the configuration directory of the user: dir when it is an
// absolute path, home/.config otherwise. Empty and relative values are ignored,
// as the specification demands.
func ConfigHome(home, dir string) string {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(home, ".config")
}
