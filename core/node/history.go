package node

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/store"
)

// payloads reads the payloads of the history from the blob store.
type payloads struct {
	blobs *blob.Store
}

// Has reports whether the payload is stored on this device.
func (p payloads) Has(ctx context.Context, hash [32]byte) (bool, error) {
	return p.blobs.Has(ctx, hash)
}

// Open returns a reader of one stored payload.
func (p payloads) Open(ctx context.Context, hash [32]byte) (history.Payload, error) {
	return p.blobs.Open(ctx, hash)
}

// openHistory creates the history of the node.
func (n *Node) openHistory(db *store.Store, origin model.DeviceID) (*history.Store, error) {
	hist, err := history.New(history.Options{
		Store:     db,
		Origin:    origin,
		Payloads:  payloads{blobs: n.blobs},
		Clipboard: n.cfg.Clipboard,
		Fetch:     n.fetchPayload,
		Clock:     n.clock,
		Logger:    n.log,
		OnChange:  n.historyChanged,
	})
	if err != nil {
		return nil, err
	}
	n.history = hist
	return hist, nil
}

// historyChanged reports a changed history to the subscribers. Every writer
// goes through it, including the history exchange, so a reader never keeps a
// list that misses entries.
func (n *Node) historyChanged() {
	n.publish(Event{Kind: EventHistory})
}

// History returns the history of the node. It is nil before Start.
func (n *Node) History() *history.Store {
	return n.history
}

// ReloadTTLs rereads the stored history lifetimes, so a saved change applies to
// the next captured item without a restart. Items already recorded keep their
// expiry.
func (n *Node) ReloadTTLs(ctx context.Context) error {
	if n.clip == nil {
		return errors.New("node: clipboard pipeline is not ready")
	}
	ttls, err := history.LoadTTLs(ctx, n.db.Settings())
	n.clip.SetTTLs(clip.TTLs{Text: ttls.Text, Image: ttls.Image, Files: ttls.Files})
	if err != nil {
		return fmt.Errorf("node: read the history lifetimes: %w", err)
	}
	n.log.Info("history lifetimes changed",
		"text", ttls.Text.String(), "image", ttls.Image.String(), "files", ttls.Files.String())
	return nil
}

// TTLs returns the history lifetimes the pipeline uses now.
func (n *Node) TTLs() clip.TTLs {
	if n.clip == nil {
		return clip.TTLs{}
	}
	return n.clip.TTLs()
}

// installHistory registers the history exchange on a new session.
func (n *Node) installHistory(_ context.Context, sess *session.Session) error {
	return n.history.Register(sess)
}

// startHistorySync sends the local version vector to a peer that may exchange
// the history. The answer is handled by the session handlers.
func (n *Node) startHistorySync(sess *session.Session) {
	if n.history == nil {
		return
	}

	n.spawn(func() {
		if err := n.history.Sync(n.ctx, sess); err != nil && sess.Err() == nil {
			n.log.Debug("history exchange did not start", "peer", sess.DeviceID(), "error", err)
		}
	})
}

// historyRun keeps the history cleanup running until the node stops.
func (n *Node) historyRun(ctx context.Context) {
	if err := n.history.Run(ctx); err != nil && ctx.Err() == nil {
		n.log.Error("history cleanup stopped", "error", err)
	}
}

// fetchPayload downloads one history payload from the online peers. The
// payload may not be larger than the size the peer announced; an announced
// empty payload still gets a bound.
func (n *Node) fetchPayload(ctx context.Context, hash [32]byte, size uint64) error {
	peers := n.BlobPeers()
	if len(peers) == 0 {
		return history.ErrNoSource
	}
	request := transfer.FetchRequest{
		Hash:    hash,
		Peers:   peers,
		MaxSize: max(int64(min(size, math.MaxInt64)), 1),
	}
	if _, err := n.fetcher.Fetch(ctx, request); err != nil {
		if errors.Is(err, transfer.ErrNoSource) {
			return history.ErrNoSource
		}
		return fmt.Errorf("node: fetch history payload: %w", err)
	}
	return nil
}
