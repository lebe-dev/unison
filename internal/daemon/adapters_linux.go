//go:build linux

package daemon

import (
	"errors"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformLinux

// errNotImplemented reports a platform backend the daemon does not have yet.
var errNotImplemented = errors.New("daemon: the keystore and the clipboard are not implemented yet on Linux")

// NewKeystore always fails until the Linux keystore exists.
func NewKeystore(string) (ports.Keystore, error) { return nil, errNotImplemented }

// NewClipboard always fails until the Linux clipboard exists.
func NewClipboard() (ports.Clipboard, error) { return nil, errNotImplemented }
