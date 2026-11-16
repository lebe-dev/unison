package node

import (
	"context"

	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/store"
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

// openShare creates the file transfer service of the node.
func (n *Node) openShare(db *store.Store) error {
	service, err := share.New(share.Options{
		Blobs:      n.blobs,
		Trust:      n.trust,
		Fetcher:    n.fetcher,
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
