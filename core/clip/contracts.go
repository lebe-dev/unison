package clip

import (
	"context"
	"sync"

	"github.com/lebe-dev/unison/core/model"
)

// Announcer delivers captured items to the peers that may receive them.
type Announcer interface {
	// Announce sends item to the remote devices. An item is announced once,
	// right after it is created.
	Announce(ctx context.Context, item model.ClipItem) error
}

// Decision is the outcome of the policy for an incoming item.
type Decision uint8

// Policy outcomes.
const (
	// DecisionSkip leaves the item out of the clipboard.
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

// HistorySink records clip items.
type HistorySink interface {
	// Append records item as the most recent entry.
	Append(ctx context.Context, item model.ClipItem) error
	// Last returns the most recent item and whether there is one.
	Last() (model.ClipItem, bool)
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
