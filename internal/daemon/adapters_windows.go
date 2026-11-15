//go:build windows

package daemon

import (
	"path/filepath"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/windows/clipboard"
	"github.com/lebe-dev/unison/platform/windows/keystore"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformWindows

// NewKeystore returns the DPAPI keystore of a data directory.
func NewKeystore(dataDir string) (ports.Keystore, error) {
	return keystore.New(filepath.Join(dataDir, KeysDirName), nil)
}

// NewClipboard returns the clipboard of this platform.
func NewClipboard() (ports.Clipboard, error) {
	return clipboard.New(clipboard.Options{})
}
