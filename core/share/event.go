package share

import "github.com/lebe-dev/unison/core/model"

// EventKind classifies a transfer event.
type EventKind uint8

// Transfer event kinds.
const (
	// EventOffered reports an offer that was sent to a peer.
	EventOffered EventKind = iota + 1
	// EventPending reports an incoming offer that waits for a decision.
	EventPending
	// EventAccepted reports an accepted offer.
	EventAccepted
	// EventProgress reports chunks that moved.
	EventProgress
	// EventSaved reports one file written by the receiver.
	EventSaved
	// EventDone reports a finished transfer.
	EventDone
	// EventDeclined reports an offer the receiver declined.
	EventDeclined
	// EventCanceled reports a transfer either side stopped.
	EventCanceled
	// EventFailed reports a transfer that could not finish.
	EventFailed
)

// String returns the name of the event kind.
func (k EventKind) String() string {
	switch k {
	case EventOffered:
		return "offered"
	case EventPending:
		return "pending"
	case EventAccepted:
		return "accepted"
	case EventProgress:
		return "progress"
	case EventSaved:
		return "saved"
	case EventDone:
		return "done"
	case EventDeclined:
		return "declined"
	case EventCanceled:
		return "canceled"
	case EventFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Event is one change of a transfer.
type Event struct {
	// Kind is the change.
	Kind EventKind
	// Peer is the device at the other side.
	Peer model.DeviceID
	// Transfer is the state after the change.
	Transfer Transfer
	// Path is the written file of an EventSaved event.
	Path string
}
