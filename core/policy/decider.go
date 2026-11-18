package policy

import (
	"context"
	"errors"
	"math"
	"sync/atomic"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
)

// PeerFunc returns the trust state of a peer. An error means the item cannot be
// handled at all.
type PeerFunc func(ctx context.Context, id model.DeviceID) (Peer, error)

// NetworkFunc returns the current network state of the local device.
type NetworkFunc func(ctx context.Context) model.NetworkState

// PathFunc returns the path used to reach a peer.
type PathFunc func(ctx context.Context, id model.DeviceID) model.Path

// Options configures a Decider.
type Options struct {
	// Peer resolves the trust state of a peer.
	Peer PeerFunc
	// Network returns the network state of the local device.
	Network NetworkFunc
	// Path returns the connection path of a peer.
	Path PathFunc
}

// Decider applies a policy configuration to incoming items.
type Decider struct {
	peer    PeerFunc
	network NetworkFunc
	path    PathFunc
	config  atomic.Pointer[Config]
}

var _ clip.PolicyDecider = (*Decider)(nil)

// New creates a decider with the default configuration. Use SetConfig to apply
// overrides.
func New(opts Options) (*Decider, error) {
	if opts.Peer == nil {
		return nil, errors.New("policy: peer source is required")
	}
	if opts.Network == nil {
		return nil, errors.New("policy: network source is required")
	}
	if opts.Path == nil {
		return nil, errors.New("policy: path source is required")
	}
	d := &Decider{peer: opts.Peer, network: opts.Network, path: opts.Path}
	d.SetConfig(Default())
	return d, nil
}

// Config returns the current configuration.
func (d *Decider) Config() Config {
	cfg := d.config.Load()
	if cfg == nil {
		return Default()
	}
	return *cfg
}

// SetConfig replaces the configuration. The value is copied, so later changes
// of cfg do not affect the decider.
func (d *Decider) SetConfig(cfg Config) {
	d.config.Store(&cfg)
}

// DecideItem returns the outcome for item received from a peer. Source is what
// triggered the local decision: an announcement, a share offer or an explicit
// request of the user.
func (d *Decider) DecideItem(ctx context.Context, from model.DeviceID, item model.ClipItem, source model.Source) (Result, error) {
	peer, err := d.peer(ctx, from)
	if err != nil {
		return Result{}, err
	}
	return Decide(d.Config(), Input{
		Peer:    peer,
		Network: d.network(ctx),
		Path:    d.path(ctx, from),
		Kind:    item.Kind,
		Size:    ItemSize(item),
		Source:  source,
	}), nil
}

// DecideShare returns the outcome for files of size bytes a peer offers. The
// trust store already decided whether the files of the peer wait for the user,
// so the clipboard confirmation flag of the peer does not apply here.
func (d *Decider) DecideShare(ctx context.Context, from model.DeviceID, size int64) (Result, error) {
	peer, err := d.peer(ctx, from)
	if err != nil {
		return Result{}, err
	}
	peer.RequireConfirmation = false
	return Decide(d.Config(), Input{
		Peer:    peer,
		Network: d.network(ctx),
		Path:    d.path(ctx, from),
		Kind:    model.KindFiles,
		Size:    size,
		Source:  model.SourceShare,
	}), nil
}

// Decide reports whether an announced item reaches the local clipboard. Items
// that need confirmation or are kept as metadata are not applied.
func (d *Decider) Decide(ctx context.Context, from model.DeviceID, item model.ClipItem) (clip.Decision, error) {
	result, err := d.DecideItem(ctx, from, item, model.SourceClipboard)
	if err != nil {
		return clip.DecisionSkip, err
	}
	if result.Action != ActionAutoApply {
		return clip.DecisionSkip, nil
	}
	return clip.DecisionApply, nil
}

// ItemSize returns the total payload size of item in bytes. It saturates
// instead of overflowing.
func ItemSize(item model.ClipItem) int64 {
	var total int64
	for _, rep := range item.Representations {
		size := rep.Size
		if size == 0 && len(rep.Inline) > 0 {
			size = uint64(len(rep.Inline))
		}
		if size > math.MaxInt64-uint64(total) {
			return math.MaxInt64
		}
		total += int64(size)
	}
	return total
}
