package node

import (
	"log/slog"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/share"
)

// EventKind classifies a node event.
type EventKind uint8

// Node event kinds.
const (
	// EventPeerOnline reports that a session with a paired device is open.
	EventPeerOnline EventKind = iota + 1
	// EventPeerOffline reports that the session with a paired device ended.
	EventPeerOffline
	// EventClipboard reports a clipboard item that this device captured or one
	// that a peer announced and the pipeline applied.
	EventClipboard
	// EventClipDownload reports a clipboard item a peer announced that is kept
	// in the history until the user downloads it.
	EventClipDownload
	// EventClipApply reports a clipboard item a peer announced that waits for
	// the user to apply it.
	EventClipApply
	// EventPeerIncompatible reports a peer that speaks another protocol
	// version and has to be updated.
	EventPeerIncompatible
	// EventPairingCandidate reports a device that is open for pairing.
	EventPairingCandidate
	// EventPairingRequest reports a device that asked to pair. The SAS is ready
	// and the user confirms or rejects it.
	EventPairingRequest
	// EventPairingPaired reports a completed pairing.
	EventPairingPaired
	// EventPairingFailed reports a pairing exchange that ended without a new
	// device.
	EventPairingFailed
	// EventPairingClosed reports a closed pairing window.
	EventPairingClosed
	// EventTransfer reports a change of a file transfer.
	EventTransfer
	// EventHistory reports a change of the history: an entry was recorded,
	// deleted or expired, whoever produced it.
	EventHistory
	// EventDeviceUpdated reports changed confirmation flags of a paired
	// device, on this device or on that one.
	EventDeviceUpdated
	// EventPeerUnreachable reports an offline paired device that stopped
	// answering on its known addresses. Err lists the addresses and the
	// failures.
	EventPeerUnreachable
)

// String returns the name of the event kind.
func (k EventKind) String() string {
	switch k {
	case EventPeerOnline:
		return "peer-online"
	case EventPeerOffline:
		return "peer-offline"
	case EventClipboard:
		return "clipboard"
	case EventClipDownload:
		return "clip-download"
	case EventClipApply:
		return "clip-apply"
	case EventPeerIncompatible:
		return "peer-incompatible"
	case EventPairingCandidate:
		return "pairing-candidate"
	case EventPairingRequest:
		return "pairing-request"
	case EventPairingPaired:
		return "pairing-paired"
	case EventPairingFailed:
		return "pairing-failed"
	case EventPairingClosed:
		return "pairing-closed"
	case EventTransfer:
		return "transfer"
	case EventHistory:
		return "history"
	case EventDeviceUpdated:
		return "device-updated"
	case EventPeerUnreachable:
		return "peer-unreachable"
	default:
		return "unknown"
	}
}

// Event is what the node reports to its subscribers. The fields that apply
// depend on Kind.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// Peer is the device the event is about.
	Peer model.DeviceID
	// Name is the display name of the peer when known.
	Name string
	// Role is the trust role of the peer.
	Role model.Role
	// Platform is the operating system of the peer.
	Platform model.Platform
	// Path is the network path of an online peer.
	Path model.Path
	// Local reports that a clipboard item was captured on this device instead
	// of arriving from a peer.
	Local bool
	// Item is the clipboard item, set on EventClipboard.
	Item model.ClipItem
	// Addr is the address of a pairing candidate, set on EventPairingCandidate.
	Addr string
	// SAS is the short authentication string to show, set on
	// EventPairingRequest of a SAS exchange.
	SAS pairing.SAS
	// Method is how a pairing exchange was started, set on the pairing events
	// of an exchange.
	Method pairing.Method
	// Version is the protocol version of the peer, set on
	// EventPeerIncompatible.
	Version uint32
	// Transfer is the file transfer, set on EventTransfer.
	Transfer share.Transfer
	// Err reports a failure.
	Err error
	// At is when the event happened.
	At time.Time
}

// Subscriber receives the events of a node. Every subscriber has its own queue,
// so a slow reader misses events instead of holding up the node.
type Subscriber struct {
	events chan Event
	bus    *eventBus

	once sync.Once
}

// Events returns the queue of the subscriber. The channel is closed by Close.
func (s *Subscriber) Events() <-chan Event {
	return s.events
}

// Close unsubscribes and closes the event channel. It is safe to call more than
// once.
func (s *Subscriber) Close() {
	s.once.Do(func() {
		s.bus.remove(s)
		close(s.events)
	})
}

// eventBus fans events out to the subscribers.
type eventBus struct {
	log    *slog.Logger
	buffer int

	mu   sync.RWMutex
	subs map[*Subscriber]struct{}
}

// newEventBus creates a bus with the given queue size per subscriber.
func newEventBus(buffer int, log *slog.Logger) *eventBus {
	return &eventBus{
		log:    log,
		buffer: buffer,
		subs:   make(map[*Subscriber]struct{}),
	}
}

// subscribe adds a subscriber.
func (b *eventBus) subscribe() *Subscriber {
	s := &Subscriber{events: make(chan Event, b.buffer), bus: b}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// remove drops a subscriber. The caller closes its channel.
func (b *eventBus) remove(s *Subscriber) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

// publish delivers an event to every subscriber without blocking. A subscriber
// whose queue is full misses the event.
func (b *eventBus) publish(event Event) {
	if event.At.IsZero() {
		event.At = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		select {
		case s.events <- event:
		default:
			b.log.Debug("node event dropped", "event", event.Kind.String())
		}
	}
}
