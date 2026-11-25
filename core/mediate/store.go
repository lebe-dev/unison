package mediate

import (
	"context"
	"log/slog"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/trust"
)

// Store is the peer store the service reads and writes. *trust.Store
// implements it.
type Store interface {
	// Get returns the peer record, including a revoked one.
	Get(ctx context.Context, id model.DeviceID) (trust.Peer, error)
	// Add stores a new peer.
	Add(ctx context.Context, peer trust.Peer) error
	// SetAnnouncedName records the name a peer announced. A name the user
	// chose on this device is kept.
	SetAnnouncedName(ctx context.Context, id model.DeviceID, name string) error
	// SetPlatform records the platform a peer announced.
	SetPlatform(ctx context.Context, id model.DeviceID, platform model.Platform) error
	// SetRendezvousKey stores the relay rendezvous key of a pair.
	SetRendezvousKey(ctx context.Context, id model.DeviceID, key []byte) error
	// UpdateAddrs replaces the cached addresses of a peer.
	UpdateAddrs(ctx context.Context, id model.DeviceID, addrs []string) error
	// AddIntroduction stores a signed statement of this device about a
	// Personal peer.
	AddIntroduction(ctx context.Context, intro trust.Introduction) (bool, error)
}

var _ Store = (*trust.Store)(nil)

// Sessions hands out the open sessions of the node.
type Sessions interface {
	// Session returns the open session with the device, or nil when the
	// device is offline.
	Session(id model.DeviceID) *session.Session
}

// Options configures a Service.
type Options struct {
	// Identity is the local device identity. It is required.
	Identity *identity.Identity
	// Trust is the peer store. It is required.
	Trust Store
	// Sessions hands out the open sessions. It is required.
	Sessions Sessions
	// Name is the display name this device announces to the new peer when
	// NameFunc is nil.
	Name string
	// NameFunc returns the current display name this device announces to the
	// new peer. When nil, Name is used.
	NameFunc func() string
	// Platform is the operating system of this device.
	Platform model.Platform
	// ListenAddrs returns the local addresses a peer may dial. The addresses
	// travel as hints in the exchange.
	ListenAddrs func() []string
	// LoopbackHint reports that the session with the device runs over
	// loopback, so the address hints of that exchange may be loopback too.
	LoopbackHint func(id model.DeviceID) bool
	// Events receives the exchange events. Nothing is reported when nil.
	Events EventSink
	// Completed runs after a new pair is written to the trust store.
	Completed func(ctx context.Context, exchange Exchange)
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// Logger receives service events. Nothing is logged when nil.
	Logger *slog.Logger
	// TTL is how long an exchange waits for both users. DefaultTTL is used
	// when zero.
	TTL time.Duration
}
