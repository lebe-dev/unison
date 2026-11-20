//go:build !darwin && !windows && !linux

package daemon

import (
	"errors"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformUnknown

// ClipboardModes lists the modes of the clipboard flag on this platform.
var ClipboardModes = []string{ClipboardSystem, ClipboardNone}

// errUnsupported reports a platform the daemon does not run on.
var errUnsupported = errors.New("daemon: the keystore and the clipboard are available on macOS, Windows and Linux only")

// NewKeystore always fails on this platform.
func NewKeystore(string) (ports.Keystore, error) { return nil, errUnsupported }

// NewClipboard always fails on this platform.
func NewClipboard(string) (ports.Clipboard, error) { return nil, errUnsupported }

// keystoreBackend names the keystore of the daemon for the status.
func keystoreBackend(ports.Keystore) string { return "" }

// clipboardStatus reports the backend of the clipboard and the reason of the
// manual mode.
func clipboardStatus(clip ports.Clipboard) (string, string) {
	if _, ok := clip.(noClipboard); ok {
		return ClipboardNone, "the clipboard is turned off"
	}
	return ClipboardNone, ""
}
