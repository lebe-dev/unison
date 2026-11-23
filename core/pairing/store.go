package pairing

import (
	"context"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
)

// Store is the part of the trust store the pairing window uses. The trust store
// implements it.
type Store interface {
	// Get returns the peer with the given device ID.
	Get(ctx context.Context, id model.DeviceID) (trust.Peer, error)
	// List returns the stored peers.
	List(ctx context.Context, filter trust.ListFilter) ([]trust.Peer, error)
	// Add stores a new peer.
	Add(ctx context.Context, peer trust.Peer) error
	// Rename sets the display name of a peer.
	Rename(ctx context.Context, id model.DeviceID, name string) error
	// AddIntroduction stores a signed introduction of the local device.
	AddIntroduction(ctx context.Context, intro trust.Introduction) (bool, error)
	// ApplyIntroduction verifies an introduction and adds the introduced
	// device.
	ApplyIntroduction(ctx context.Context, intro trust.Introduction) (bool, error)
	// EnsureDiscoveryKey returns the key of the Personal group, generating it
	// when the group has none.
	EnsureDiscoveryKey(ctx context.Context) ([]byte, error)
	// SetDiscoveryKey stores the key of the Personal group.
	SetDiscoveryKey(ctx context.Context, key []byte) error
	// SetRendezvousKey stores the relay rendezvous key shared with a peer.
	SetRendezvousKey(ctx context.Context, id model.DeviceID, key []byte) error
}
