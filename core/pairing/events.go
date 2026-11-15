package pairing

import (
	"github.com/lebe-dev/unison/core/model"
)

// EventKind classifies a pairing event.
type EventKind uint8

// Pairing events.
const (
	// EventCandidate reports a device that is open for pairing on the local
	// network. The address is passed to Dial.
	EventCandidate EventKind = iota + 1
	// EventRequest reports a device that asked to pair. The SAS is ready and
	// the user must confirm or reject it.
	EventRequest
	// EventPaired reports a completed pairing. Both devices are written to the
	// trust store.
	EventPaired
	// EventFailed reports an exchange that ended without a new peer.
	EventFailed
	// EventClosed reports that the window is closed and its listener is gone.
	EventClosed
)

// String returns the name of the event kind.
func (k EventKind) String() string {
	switch k {
	case EventCandidate:
		return "candidate"
	case EventRequest:
		return "request"
	case EventPaired:
		return "paired"
	case EventFailed:
		return "failed"
	case EventClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Event is what the pairing window reports to the UI.
type Event struct {
	// Kind is the type of the event.
	Kind EventKind
	// Addr is the candidate address, set on EventCandidate.
	Addr string
	// TXT is the candidate service metadata, set on EventCandidate.
	TXT map[string]string
	// DeviceID identifies the peer of a request or of a result.
	DeviceID model.DeviceID
	// Name is the display name of the peer.
	Name string
	// Platform is the operating system of the peer.
	Platform model.Platform
	// Role is the role the initiating device proposed. Both devices store it.
	Role model.Role
	// SAS is the short authentication string to show, set on EventRequest.
	SAS SAS
	// Err reports why an exchange failed, set on EventFailed.
	Err error
}
