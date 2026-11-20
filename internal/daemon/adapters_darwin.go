//go:build darwin

package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/internal/keystorename"
	"github.com/lebe-dev/unison/platform/darwin/clipboard"
	"github.com/lebe-dev/unison/platform/darwin/keystore"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformMacOS

// ClipboardModes lists the modes of the clipboard flag on this platform.
var ClipboardModes = []string{ClipboardSystem, ClipboardNone}

// NewKeystore returns the Keychain keystore of a data directory.
func NewKeystore(dataDir string) (ports.Keystore, error) {
	if !cgoAvailable {
		return nil, errors.New("daemon: the Keychain needs a build with cgo, set CGO_ENABLED=1")
	}
	return keystore.New(keychainService(dataDir))
}

// NewClipboard returns the pasteboard clipboard of this platform.
func NewClipboard(mode string) (ports.Clipboard, error) {
	switch mode {
	case "", ClipboardSystem, ClipboardAuto:
	case ClipboardNone:
		return NoClipboard(), nil
	default:
		return nil, fmt.Errorf("clipboard: %q is available on Linux only, use %s or %s", mode, ClipboardSystem, ClipboardNone)
	}
	if !cgoAvailable {
		return nil, errors.New("daemon: the pasteboard needs a build with cgo, set CGO_ENABLED=1")
	}
	pasteboard, err := clipboard.New(clipboard.Options{})
	if err != nil {
		return nil, err
	}
	// The policy is informational: a failed lookup leaves the clipboard usable.
	behavior := behaviorUnknown
	if reported, err := pasteboard.AccessBehavior(context.Background()); err == nil {
		behavior = reported
	}
	return &policyClipboard{Clipboard: pasteboard, behavior: behavior}, nil
}

// keystoreBackend names the keystore of the daemon for the status.
func keystoreBackend(ports.Keystore) string { return "keychain" }

// clipboardStatus reports the backend of the clipboard and the reason of the
// manual mode.
func clipboardStatus(clip ports.Clipboard) (string, string) {
	if _, ok := clip.(noClipboard); ok {
		return ClipboardNone, "the clipboard is turned off"
	}
	return "pasteboard", ""
}

// behaviorUnknown reports a policy the system did not report.
const behaviorUnknown clipboard.Behavior = -1

// policyClipboard is the pasteboard together with the permission policy of the
// system, which the daemon reports at startup.
type policyClipboard struct {
	*clipboard.Clipboard
	behavior clipboard.Behavior
}

// StartupWarning returns a message about a policy that keeps the daemon from
// reading the clipboard, or an empty string.
func (c *policyClipboard) StartupWarning(context.Context) string {
	switch c.behavior {
	case clipboard.BehaviorAsk, clipboard.BehaviorDefault:
		return "macOS may ask for permission to read the clipboard, allow Unison in System Settings, Privacy and Security, Paste from Other Apps"
	case clipboard.BehaviorAlwaysDeny:
		return "macOS denies reading the clipboard, allow Unison in System Settings, Privacy and Security, Paste from Other Apps"
	default:
		return ""
	}
}

// keychainService returns the Keychain service of a data directory. The default
// data directory uses the shared service, every other directory a name of its
// own, so two daemons on one machine keep separate keys.
func keychainService(dataDir string) string {
	def, err := DefaultDataDir()
	if err != nil {
		def = ""
	}
	return keystorename.Service(dataDir, def)
}
