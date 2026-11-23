package node

import (
	"context"
	"errors"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/trust"
)

// shareEvents publishes the file transfer events.
type shareEvents struct {
	node *Node
}

var _ share.EventSink = shareEvents{}

// Report delivers one transfer event to the subscribers.
func (s shareEvents) Report(_ context.Context, event share.Event) {
	s.node.publish(Event{
		Kind:     EventTransfer,
		Peer:     event.Peer,
		Transfer: event.Transfer,
	})
}

// sharePolicy applies the network policy of the node to the file offers.
type sharePolicy struct {
	node *Node
}

var _ share.Policy = sharePolicy{}

// DecideShare returns the decision of the network policy for an offer. Files
// above the limit of the network wait for the user, like a metadata item of
// the clipboard.
func (p sharePolicy) DecideShare(ctx context.Context, peer model.DeviceID, size int64) (trust.Decision, error) {
	decider := p.node.policy
	if decider == nil {
		return trust.Confirm, errors.New("node: policy is not ready")
	}
	result, err := decider.DecideShare(ctx, peer, size)
	if err != nil {
		return trust.Confirm, err
	}
	switch result.Action {
	case policy.ActionAutoApply:
		return trust.Allow, nil
	case policy.ActionReject:
		return trust.Deny, nil
	default:
		return trust.Confirm, nil
	}
}

// openShare creates the file transfer service of the node.
func (n *Node) openShare(db *store.Store) error {
	service, err := share.New(share.Options{
		Blobs:      n.blobs,
		Trust:      n.trust,
		Fetcher:    n.fetcher,
		Policy:     sharePolicy{node: n},
		Settings:   db.Settings(),
		Downloads:  n.cfg.ShareDownloads,
		Sink:       n.cfg.ShareSink,
		Events:     shareEvents{node: n},
		Clock:      n.clock,
		PendingTTL: n.cfg.SharePendingTTL,
		Logger:     n.log,
	})
	if err != nil {
		return err
	}
	n.share = service
	return nil
}

// installShare registers the file transfer handlers on a new session.
func (n *Node) installShare(_ context.Context, sess *session.Session) error {
	return n.share.Register(sess)
}

// Share returns the file transfer service of the node. It is nil before Start.
func (n *Node) Share() *share.Service {
	return n.share
}
