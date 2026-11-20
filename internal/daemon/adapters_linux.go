//go:build linux

package daemon

import (
	"errors"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/linux/keystore"
)

// Platform is the platform this build reports to peers.
const Platform = model.PlatformLinux

// errNotImplemented reports a platform backend the daemon does not have yet.
var errNotImplemented = errors.New("daemon: the keystore and the clipboard are not implemented yet on Linux")

// NewKeystore returns the keystore of a data directory: the Secret Service of
// the session when it has one, files with 0600 permissions otherwise.
func NewKeystore(dataDir string) (ports.Keystore, error) {
	def, err := DefaultDataDir()
	if err != nil {
		def = ""
	}
	return keystore.Open(dataDir, keystore.Service(dataDir, def), keystore.Options{})
}

// NewClipboard always fails until the Linux clipboard exists.
func NewClipboard() (ports.Clipboard, error) { return nil, errNotImplemented }
