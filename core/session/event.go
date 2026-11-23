package session

import "github.com/lebe-dev/unison/core/model"

// EventKind is the kind of a session event.
type EventKind uint8

// Session event kinds.
const (
	// EventOpened reports a session that completed the Hello exchange.
	EventOpened EventKind = iota + 1
	// EventClosed reports a session that ended. A reader that missed it sees a
	// closed Events channel.
	EventClosed
	// EventPeerNeedsUpdate reports a peer with an incompatible protocol version.
	EventPeerNeedsUpdate
)

// String returns the name of the event kind.
func (k EventKind) String() string {
	switch k {
	case EventOpened:
		return "opened"
	case EventClosed:
		return "closed"
	case EventPeerNeedsUpdate:
		return "peer-needs-update"
	default:
		return "unknown"
	}
}

// Event reports a session lifecycle change.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// Peer is the device the session talks to.
	Peer model.DeviceID
	// Version is the protocol version the peer announced. It is set on
	// EventPeerNeedsUpdate.
	Version uint32
	// Err is why the session ended. It is set on EventClosed and nil when the
	// session ended cleanly.
	Err error
}
