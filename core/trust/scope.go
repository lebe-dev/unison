package trust

import (
	"fmt"

	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Scope is the content a confirmation flag covers.
type Scope uint8

// Confirmation scopes.
const (
	// ScopeNone is the zero value; no flag covers it.
	ScopeNone Scope = iota
	// ScopeClipboard covers the clipboard items of a peer.
	ScopeClipboard
	// ScopeFiles covers the file transfers of a peer.
	ScopeFiles
)

// String returns the name of the scope.
func (s Scope) String() string {
	switch s {
	case ScopeClipboard:
		return "clipboard"
	case ScopeFiles:
		return "files"
	default:
		return "none"
	}
}

// Valid reports whether the scope names content a flag covers.
func (s Scope) Valid() bool {
	return s == ScopeClipboard || s == ScopeFiles
}

// ScopeFromProto converts the wire scope. An unknown value is an error.
func ScopeFromProto(scope wirev1.ConfirmationScope) (Scope, error) {
	switch scope {
	case wirev1.ConfirmationScope_CONFIRMATION_SCOPE_CLIPBOARD:
		return ScopeClipboard, nil
	case wirev1.ConfirmationScope_CONFIRMATION_SCOPE_FILES:
		return ScopeFiles, nil
	default:
		return ScopeNone, fmt.Errorf("%w: unknown confirmation scope %d", ErrInvalidPeer, scope)
	}
}

// Proto returns the wire form of the scope.
func (s Scope) Proto() wirev1.ConfirmationScope {
	switch s {
	case ScopeClipboard:
		return wirev1.ConfirmationScope_CONFIRMATION_SCOPE_CLIPBOARD
	case ScopeFiles:
		return wirev1.ConfirmationScope_CONFIRMATION_SCOPE_FILES
	default:
		return wirev1.ConfirmationScope_CONFIRMATION_SCOPE_UNSPECIFIED
	}
}

// Confirmations are the flags of one direction of a pair: whether the device
// that receives asks its user before it takes the clipboard items and the file
// transfers of the other one.
type Confirmations struct {
	// Clipboard holds the clipboard items until the user applies them.
	Clipboard bool
	// Files holds the file transfers until the user accepts them.
	Files bool
}

// Get returns the flag of the scope.
func (c Confirmations) Get(scope Scope) bool {
	switch scope {
	case ScopeClipboard:
		return c.Clipboard
	case ScopeFiles:
		return c.Files
	default:
		return false
	}
}

// With returns the flags with the flag of the scope set to require.
func (c Confirmations) With(scope Scope, require bool) Confirmations {
	switch scope {
	case ScopeClipboard:
		c.Clipboard = require
	case ScopeFiles:
		c.Files = require
	}
	return c
}

// Any reports whether any flag is on.
func (c Confirmations) Any() bool {
	return c.Clipboard || c.Files
}

// PeerPolicyProto returns the wire form of the flags.
func (c Confirmations) PeerPolicyProto() *wirev1.PeerPolicy {
	return &wirev1.PeerPolicy{
		RequireConfirmation:     c.Clipboard,
		RequireFileConfirmation: c.Files,
	}
}

// ConfirmationsFromProto reads the wire form of the flags.
func ConfirmationsFromProto(policy *wirev1.PeerPolicy) Confirmations {
	return Confirmations{
		Clipboard: policy.GetRequireConfirmation(),
		Files:     policy.GetRequireFileConfirmation(),
	}
}
