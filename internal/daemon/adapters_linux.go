//go:build linux

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/linux/clipboard"
	"github.com/lebe-dev/unison/platform/linux/keystore"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformLinux

// ClipboardModes lists the modes of the clipboard flag on this platform.
var ClipboardModes = []string{ClipboardSystem, ClipboardAuto, ClipboardX11, ClipboardWayland, ClipboardNone}

// NewKeystore returns the keystore of a data directory: the Secret Service of
// the session when it has one, files with 0600 permissions otherwise.
func NewKeystore(dataDir string) (ports.Keystore, error) {
	def, err := DefaultDataDir()
	if err != nil {
		def = ""
	}
	return keystore.Open(dataDir, keystore.Service(dataDir, def), keystore.Options{})
}

// NewClipboard returns the clipboard of the session. The daemon keeps running
// without a display and reports the reason in its status.
func NewClipboard(mode string) (ports.Clipboard, error) {
	switch mode {
	case "", ClipboardSystem:
		mode = ClipboardAuto
	}
	switch mode {
	case ClipboardAuto, ClipboardX11:
		return newManagedClipboard(mode, systemClipboardEnvironment()), nil
	case ClipboardWayland:
		return nil, errWaylandUnavailable
	case ClipboardNone:
		return NoClipboard(), nil
	default:
		return nil, fmt.Errorf("clipboard: unknown mode %q", mode)
	}
}

// keystoreBackend names the keystore of the daemon for the status.
func keystoreBackend(store ports.Keystore) string {
	k, ok := store.(*keystore.Keystore)
	if !ok {
		return ""
	}
	return string(k.Backend())
}

// clipboardStatus reports the backend of the clipboard and the reason of the
// manual mode.
func clipboardStatus(clip ports.Clipboard) (string, string) {
	if _, ok := clip.(noClipboard); ok {
		return ClipboardNone, "the clipboard is turned off"
	}
	reporter, ok := clip.(clipboardStatusReporter)
	if !ok {
		return ClipboardNone, ""
	}
	return reporter.ClipboardStatus()
}

// clipboardEnvironment reaches the display of the session. Tests replace it.
type clipboardEnvironment struct {
	getenv     func(string) string
	probe      func(context.Context, string) error
	newX11     func(string) (ports.Clipboard, error)
	newWayland func() (ports.Clipboard, error)
}

// systemClipboardEnvironment reaches the display of the session.
func systemClipboardEnvironment() clipboardEnvironment {
	return clipboardEnvironment{
		getenv: os.Getenv,
		probe:  probeDisplay,
		newX11: func(display string) (ports.Clipboard, error) {
			return clipboard.NewX11(clipboard.X11Options{Display: display})
		},
		newWayland: newWaylandClipboard,
	}
}

// newWaylandClipboard returns the clipboard of the Wayland data-control
// protocol.
func newWaylandClipboard() (ports.Clipboard, error) { return nil, errWaylandUnavailable }

// errWaylandUnavailable reports the backend that the Wayland data-control
// protocol needs and the daemon does not have yet.
var errWaylandUnavailable = errors.New("clipboard: the Wayland data-control backend is not available yet, use x11")

// clipboardWatchBuffer is the number of snapshots buffered for a watcher. A
// slow watcher loses snapshots instead of blocking the daemon.
const clipboardWatchBuffer = 16
