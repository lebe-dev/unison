//go:build windows

package daemon

import (
	"fmt"
	"path/filepath"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/windows/clipboard"
	"github.com/lebe-dev/unison/platform/windows/keystore"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformWindows

// ClipboardModes lists the modes of the clipboard flag on this platform.
var ClipboardModes = []string{ClipboardSystem, ClipboardNone}

// NewKeystore returns the DPAPI keystore of a data directory.
func NewKeystore(dataDir string) (ports.Keystore, error) {
	return keystore.New(filepath.Join(dataDir, KeysDirName), nil)
}

// NewClipboard returns the clipboard of this platform.
func NewClipboard(mode string) (ports.Clipboard, error) {
	switch mode {
	case "", ClipboardSystem, ClipboardAuto:
		return clipboard.New(clipboard.Options{})
	case ClipboardNone:
		return NoClipboard(), nil
	default:
		return nil, fmt.Errorf("clipboard: %q is available on Linux only, use %s or %s", mode, ClipboardSystem, ClipboardNone)
	}
}

// keystoreBackend names the keystore of the daemon for the status.
func keystoreBackend(ports.Keystore) string { return "dpapi" }

// clipboardStatus reports the backend of the clipboard and the reason of the
// manual mode.
func clipboardStatus(clip ports.Clipboard) (string, string) {
	if _, ok := clip.(noClipboard); ok {
		return ClipboardNone, "the clipboard is turned off"
	}
	return "win32", ""
}
