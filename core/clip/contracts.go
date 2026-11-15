package clip

import (
	"context"
	"io"
	"sync"

	"github.com/lebe-dev/unison/core/model"
)

// Announcer delivers captured items to the peers that may receive them.
type Announcer interface {
	// Announce sends item to the remote devices. An item is announced once,
	// right after it is created.
	Announce(ctx context.Context, item model.ClipItem) error
}

// Decision is the outcome of the simple policy for an incoming item.
type Decision uint8

// Simple policy outcomes.
const (
	// DecisionSkip leaves the item out of the clipboard and the history.
	DecisionSkip Decision = iota
	// DecisionApply writes the item to the local clipboard.
	DecisionApply
)

// String returns the name of the decision.
func (d Decision) String() string {
	switch d {
	case DecisionApply:
		return "apply"
	default:
		return "skip"
	}
}

// PolicyDecider decides what happens to an item announced by a peer.
type PolicyDecider interface {
	// Decide returns the outcome for item received from from. An error means the
	// item cannot be handled at all.
	Decide(ctx context.Context, from model.DeviceID, item model.ClipItem) (Decision, error)
}

// AutoApply applies every incoming item. It is the policy of a device that has
// no policy configuration.
type AutoApply struct{}

var _ PolicyDecider = AutoApply{}

// Decide reports DecisionApply for every item.
func (AutoApply) Decide(context.Context, model.DeviceID, model.ClipItem) (Decision, error) {
	return DecisionApply, nil
}

// Action is what the pipeline does with an item announced by a peer.
type Action uint8

// Outcomes of a Planner.
const (
	// ActionSkip drops the item: it is neither recorded nor applied.
	ActionSkip Action = iota
	// ActionApply writes the item to the local clipboard.
	ActionApply
	// ActionConfirm records the item and reports it for confirmation.
	ActionConfirm
	// ActionMetadata records the item and leaves the payload to the user.
	ActionMetadata
)

// String returns the name of the action.
func (a Action) String() string {
	switch a {
	case ActionApply:
		return "apply"
	case ActionConfirm:
		return "confirm"
	case ActionMetadata:
		return "metadata"
	default:
		return "skip"
	}
}

// Outcome is the plan for one incoming item.
type Outcome struct {
	// Action is what happens to the item.
	Action Action
	// Eager reports that the payload is downloaded before the item is applied.
	Eager bool
}

// Planner decides what happens to an item announced by a peer, including
// whether its payload is downloaded before it is applied.
type Planner interface {
	// Plan returns the outcome for item received from from.
	Plan(ctx context.Context, from model.DeviceID, item model.ClipItem) (Outcome, error)
}

// policyPlanner adapts a PolicyDecider to the Planner interface. Items it
// applies are fetched eagerly.
type policyPlanner struct {
	decider PolicyDecider
}

// Plan returns the outcome of the wrapped policy.
func (p policyPlanner) Plan(ctx context.Context, from model.DeviceID, item model.ClipItem) (Outcome, error) {
	decision, err := p.decider.Decide(ctx, from, item)
	if err != nil {
		return Outcome{}, err
	}
	if decision != DecisionApply {
		return Outcome{Action: ActionSkip}, nil
	}
	return Outcome{Action: ActionApply, Eager: true}, nil
}

// Payload is the reader of one stored payload.
type Payload interface {
	// Size is the plaintext size in bytes.
	Size() int64
	// ReadAt reads the payload, verifying every chunk it touches.
	ReadAt(p []byte, off int64) (int, error)
	// Close releases the payload.
	Close() error
}

// PayloadStore keeps the payloads that do not travel inside the item.
type PayloadStore interface {
	// Put stores content of size bytes read at offsets of src and returns its
	// BLAKE3 hash.
	Put(ctx context.Context, src io.ReaderAt, size int64) ([32]byte, error)
	// Has reports whether the payload is stored on this device.
	Has(ctx context.Context, hash [32]byte) (bool, error)
	// Open returns a reader of a stored payload.
	Open(ctx context.Context, hash [32]byte) (Payload, error)
}

// Fetcher downloads one payload from the peers.
type Fetcher interface {
	// Fetch stores the payload of hash locally.
	Fetch(ctx context.Context, hash [32]byte) error
}

// FetchFunc adapts a function to the Fetcher interface.
type FetchFunc func(ctx context.Context, hash [32]byte) error

// Fetch stores the payload of hash locally.
func (f FetchFunc) Fetch(ctx context.Context, hash [32]byte) error {
	return f(ctx, hash)
}

// EventKind is what the pipeline reports about a recorded incoming item.
type EventKind uint8

// Pipeline event kinds.
const (
	// EventApplied reports an item that reached the local clipboard.
	EventApplied EventKind = iota + 1
	// EventDownload reports an item whose payload the user can download.
	EventDownload
	// EventApply reports an item that waits for the user to apply it.
	EventApply
)

// String returns the name of the event kind.
func (k EventKind) String() string {
	switch k {
	case EventApplied:
		return "applied"
	case EventDownload:
		return "download"
	case EventApply:
		return "apply"
	default:
		return "unknown"
	}
}

// Event is what the pipeline reports to the user interface.
type Event struct {
	// Kind is what happened to the item.
	Kind EventKind
	// Peer is the device that announced the item.
	Peer model.DeviceID
	// Item is the recorded item.
	Item model.ClipItem
}

// EventSink receives the events of the pipeline.
type EventSink interface {
	// Report delivers one event. It must not block.
	Report(ctx context.Context, event Event)
}

// HistorySink records clip items.
type HistorySink interface {
	// Append records item as the most recent entry.
	Append(ctx context.Context, item model.ClipItem) error
	// Last returns the most recent item and whether there is one.
	Last() (model.ClipItem, bool)
}

// PresenceSink records which payloads of the history are stored locally. The
// history implements it; other sinks may not.
type PresenceSink interface {
	// MarkPresent records the local state of one payload.
	MarkPresent(ctx context.Context, hash [32]byte, present bool) error
}

// MemoryHistory is a HistorySink that keeps the most recent item only.
type MemoryHistory struct {
	mu   sync.Mutex
	last model.ClipItem
	set  bool
}

// NewMemoryHistory creates an empty in-memory history.
func NewMemoryHistory() *MemoryHistory {
	return &MemoryHistory{}
}

var _ HistorySink = (*MemoryHistory)(nil)

// Append replaces the stored item.
func (h *MemoryHistory) Append(_ context.Context, item model.ClipItem) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last = item
	h.set = true
	return nil
}

// Last returns the stored item and whether there is one.
func (h *MemoryHistory) Last() (model.ClipItem, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last, h.set
}
