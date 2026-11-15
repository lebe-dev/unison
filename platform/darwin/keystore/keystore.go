// Package keystore stores secrets in the macOS Keychain.
//
// Secrets are generic password items: the service identifies the application,
// the account is the secret name. Items are readable after the first unlock of
// the login session, never leave the device and are not synced to iCloud.
package keystore

import (
	"errors"
	"strconv"

	"github.com/lebe-dev/unison/core/ports"
)

// DefaultService is the Keychain service Unison secrets are stored under.
const DefaultService = "app.unison"

// ErrUnsupported reports that the Keychain is unavailable because the binary
// was built without cgo.
var ErrUnsupported = errors.New("keystore: keychain requires cgo")

// Keystore is a ports.Keystore backed by the Keychain.
type Keystore struct {
	service string
}

var _ ports.Keystore = (*Keystore)(nil)

// New returns a Keystore that reads and writes the items of service. Use a
// dedicated service to keep unrelated secrets apart.
func New(service string) (*Keystore, error) {
	if service == "" {
		return nil, errors.New("keystore: empty service name")
	}
	return &Keystore{service: service}, nil
}

// Service returns the Keychain service of the keystore.
func (k *Keystore) Service() string { return k.service }

// Error reports a failed Keychain call.
type Error struct {
	// Op is the failed operation: "load", "store" or "delete".
	Op string
	// Status is the OSStatus the Keychain returned.
	Status int32
}

func (e *Error) Error() string {
	return "keystore: " + e.Op + ": keychain status " + strconv.Itoa(int(e.Status))
}
