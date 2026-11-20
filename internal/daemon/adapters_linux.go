//go:build linux

package daemon

import (
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/linux/clipboard"
	"github.com/lebe-dev/unison/platform/linux/keystore"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformLinux

// NewKeystore returns the keystore of a data directory: the Secret Service of
// the session when it has one, files with 0600 permissions otherwise.
func NewKeystore(dataDir string) (ports.Keystore, error) {
	def, err := DefaultDataDir()
	if err != nil {
		def = ""
	}
	return keystore.Open(dataDir, keystore.Service(dataDir, def), keystore.Options{})
}

// NewClipboard returns the X11 clipboard, which reaches the Wayland session
// through XWayland.
func NewClipboard() (ports.Clipboard, error) { return clipboard.NewX11(clipboard.X11Options{}) }
