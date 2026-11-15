package node

import (
	"context"
	"io"
	"path/filepath"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/trust"
)

// blobsDir is the directory of the blob files inside the data directory.
const blobsDir = "blobs"

// blobPayloads serves the payload store of the clipboard pipeline.
type blobPayloads struct {
	blobs *blob.Store
}

var _ clip.PayloadStore = blobPayloads{}

// Put stores content of size bytes and returns its BLAKE3 hash.
func (b blobPayloads) Put(ctx context.Context, src io.ReaderAt, size int64) ([32]byte, error) {
	info, err := b.blobs.Put(ctx, src, size)
	if err != nil {
		return [32]byte{}, err
	}
	return info.Hash, nil
}

// Has reports whether the payload is stored on this device.
func (b blobPayloads) Has(ctx context.Context, hash [32]byte) (bool, error) {
	return b.blobs.Has(ctx, hash)
}

// Open returns a reader of a stored payload.
func (b blobPayloads) Open(ctx context.Context, hash [32]byte) (clip.Payload, error) {
	return b.blobs.Open(ctx, hash)
}

// openBlobs opens the blob store and the transfer service of the node.
func (n *Node) openBlobs(db *store.Store) error {
	blobs, err := blob.New(blob.Options{
		Dir:    filepath.Join(n.cfg.DataDir, blobsDir),
		Store:  db,
		Limit:  n.cfg.BlobLimit,
		Clock:  n.clock,
		Logger: n.log,
	})
	if err != nil {
		return err
	}
	service, err := transfer.NewService(transfer.ServiceOptions{
		Blobs:  blobs,
		Logger: n.log,
	})
	if err != nil {
		return err
	}
	fetcher, err := transfer.NewFetcher(transfer.FetchOptions{
		Blobs:  blobs,
		Logger: n.log,
	})
	if err != nil {
		return err
	}

	n.blobs = blobs
	n.blobService = service
	n.fetcher = fetcher
	return nil
}

// installTransfer registers the blob handlers on a new session.
func (n *Node) installTransfer(_ context.Context, sess *session.Session) error {
	if err := n.blobService.Register(sess); err != nil {
		return err
	}
	return n.fetcher.Register(sess)
}

// openPolicy creates the clipboard policy decider from the stored settings.
func (n *Node) openPolicy(ctx context.Context, db *store.Store) (*policy.Decider, error) {
	decider, err := policy.New(policy.Options{
		Peer: func(ctx context.Context, id model.DeviceID) (policy.Peer, error) {
			peer, err := n.trust.Get(ctx, id)
			if err != nil {
				return policy.Peer{}, err
			}
			return policy.Peer{Role: peer.Role, RequireConfirmation: peer.RequireConfirmation}, nil
		},
		Network: func(context.Context) model.NetworkState {
			return n.Network()
		},
		Path: func(_ context.Context, id model.DeviceID) model.Path {
			return n.peerState(id).Path
		},
	})
	if err != nil {
		return nil, err
	}

	cfg, err := policy.LoadConfig(ctx, db.Settings())
	if err != nil {
		n.log.Warn("reading the clipboard policy failed, using the defaults", "error", err)
	}
	decider.SetConfig(cfg)
	return decider, nil
}

// Blobs returns the blob store of the node. It is nil before Start.
func (n *Node) Blobs() *blob.Store {
	return n.blobs
}

// Fetcher returns the blob fetcher of the node. It is nil before Start.
func (n *Node) Fetcher() *transfer.Fetcher {
	return n.fetcher
}

// Policy returns the clipboard policy of the node. It is nil before Start.
func (n *Node) Policy() *policy.Decider {
	return n.policy
}

// BlobPeers returns the online peers that may serve a blob.
func (n *Node) BlobPeers() []transfer.Peer {
	var peers []transfer.Peer
	for _, l := range n.linkList() {
		sess, peer, ok := l.onlineSession()
		if !ok || !peer.Allowed(trust.ActionFetchBlob) {
			continue
		}
		peers = append(peers, sess)
	}
	return peers
}
