package trust

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// maxNameLength bounds the display name of a peer in bytes.
const maxNameLength = 128

// Errors reported by the trust store.
var (
	// ErrPeerNotFound reports a device that is not in the store.
	ErrPeerNotFound = errors.New("trust: peer not found")
	// ErrPeerExists reports a peer that was already added.
	ErrPeerExists = errors.New("trust: peer already exists")
	// ErrPeerRevoked reports a revoked device, which stays on the deny-list.
	ErrPeerRevoked = errors.New("trust: peer is revoked")
	// ErrInvalidPeer reports an incomplete or inconsistent peer record.
	ErrInvalidPeer = errors.New("trust: invalid peer")
	// ErrSecretNotFound reports a group secret that is not stored yet.
	ErrSecretNotFound = errors.New("trust: secret not found")
	// ErrInvalidSecret reports a group secret of the wrong size.
	ErrInvalidSecret = errors.New("trust: invalid secret")
)

// Peer is a paired device with the policy the local device applies to it.
type Peer struct {
	// Public is the identity of the device, including its device ID.
	Public identity.PublicIdentity
	// Name is the display name of the device.
	Name string
	// NameLocal reports a name the user chose on this device. Such a name is not
	// replaced by the name the device announces when a session opens.
	NameLocal bool
	// Platform is the operating system of the device.
	Platform model.Platform
	// Role is the trust role assigned at pairing.
	Role model.Role
	// Confirm are the flags of this device for the peer: what this device holds
	// until the user takes it. This device changes them, on its own or at the
	// signed request of the peer.
	Confirm Confirmations
	// ConfirmRemote are the flags of the peer for this device, as the peer
	// reported them last. The peer owns them; this device only records them.
	ConfirmRemote Confirmations
	// IntroducedBy is the Personal device that introduced this peer, or the zero
	// device ID when the peer was paired directly.
	IntroducedBy model.DeviceID
	// AddedAt is when the peer was added to the store.
	AddedAt time.Time
	// LastSeenAt is when the peer was last reachable.
	LastSeenAt time.Time
	// LastAddrs are the addresses that last reached the peer, most recent
	// first. The cache is bounded by MaxAddrs.
	LastAddrs []string
	// Revoked reports a device that was revoked. Such a device stays in the
	// store as a deny-list entry and is never trusted again.
	Revoked bool
}

// DeviceID returns the device ID of the peer.
func (p Peer) DeviceID() model.DeviceID {
	return p.Public.DeviceID
}

// Validate checks the identity, the role and the display data of the peer.
func (p Peer) Validate() error {
	public, err := identity.NewPublicIdentity(p.Public.Ed25519, p.Public.MLDSABytes())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	if public.DeviceID != p.Public.DeviceID {
		return fmt.Errorf("%w: device id does not match the public keys", ErrInvalidPeer)
	}
	if p.Role != model.RolePersonal && p.Role != model.RoleFriends {
		return fmt.Errorf("%w: unknown role %d", ErrInvalidPeer, p.Role)
	}
	if p.Confirm.Any() && p.Role != model.RolePersonal {
		return fmt.Errorf("%w: confirmation applies to personal peers only", ErrInvalidPeer)
	}
	if !p.IntroducedBy.IsZero() && p.IntroducedBy == p.Public.DeviceID {
		return fmt.Errorf("%w: a peer cannot introduce itself", ErrInvalidPeer)
	}
	if err := validateName(p.Name); err != nil {
		return err
	}
	return nil
}

// validateName checks that a display name is valid UTF-8 of a bounded length.
func validateName(name string) error {
	if len(name) > maxNameLength || !utf8.ValidString(name) {
		return fmt.Errorf("%w: invalid name", ErrInvalidPeer)
	}
	return nil
}
