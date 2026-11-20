//go:build !darwin && !windows && !linux

package daemon

import (
	"errors"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformUnknown

// errUnsupported reports a platform the daemon does not run on.
var errUnsupported = errors.New("daemon: the keystore and the clipboard are available on macOS, Windows and Linux only")

// NewKeystore always fails on this platform.
func NewKeystore(string) (ports.Keystore, error) { return nil, errUnsupported }

// NewClipboard always fails on this platform.
func NewClipboard() (ports.Clipboard, error) { return nil, errUnsupported }
