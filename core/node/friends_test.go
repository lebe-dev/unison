package node

import (
	"context"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// TestShareFriendRequestTimesOut checks that an offer that nobody decides stops
// waiting, writes nothing and reaches the sender.
func TestShareFriendRequestTimesOut(t *testing.T) {
	a, b := shareNodesWith(t, 60000, model.RoleFriends, func(cfg *Config) {
		cfg.SharePendingTTL = 300 * time.Millisecond
	})

	path, _ := shareFile(t, "late.txt", 1<<20)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	pending := waitIncoming(t, b, a.DeviceID(), share.StatePending)
	if pending.ExpiresAt.IsZero() {
		t.Fatal("the request has no deadline")
	}

	waitTransfer(t, b, pending.ID, share.StateDeclined)
	if requests := b.Share().Requests(); len(requests) != 0 {
		t.Fatalf("%d requests still wait for a decision", len(requests))
	}
	waitTransfer(t, a, ids[0], share.StateDeclined)
	if names := downloads(t, b); len(names) != 0 {
		t.Fatalf("an expired offer wrote %v", names)
	}
}

// TestShareFriendPendingLimit checks that one peer cannot pile up requests
// without a decision.
func TestShareFriendPendingLimit(t *testing.T) {
	a, b := shareNodes(t, 62000, model.RoleFriends)

	path, _ := shareFile(t, "many.txt", 8)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ids := make([]string, 0, share.MaxPendingPerPeer+1)
	for range share.MaxPendingPerPeer + 1 {
		sent, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		ids = append(ids, sent...)
	}

	waitTransfer(t, a, ids[len(ids)-1], share.StateDeclined)
	deadline := time.Now().Add(testTimeout)
	for len(b.Share().Requests()) != share.MaxPendingPerPeer && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if requests := len(b.Share().Requests()); requests != share.MaxPendingPerPeer {
		t.Fatalf("the device holds %d requests, want %d", requests, share.MaxPendingPerPeer)
	}
	if names := downloads(t, b); len(names) != 0 {
		t.Fatalf("pending offers wrote %v", names)
	}
}

// TestFriendsForbiddenMessages checks that a Friends device cannot use the
// messages of the personal group.
func TestFriendsForbiddenMessages(t *testing.T) {
	hash := [32]byte{1}

	cases := []struct {
		name  string
		send  func(ctx context.Context, sess *session.Session, sender model.DeviceID) error
		check func(t *testing.T, receiver, sender *testNode)
	}{
		{
			name: "clip announce",
			send: func(ctx context.Context, sess *session.Session, sender model.DeviceID) error {
				item := model.ClipItem{
					ID:        model.ClipID{1},
					Origin:    sender,
					Seq:       1,
					CreatedAt: time.Now(),
					Kind:      model.KindText,
					Source:    model.SourceClipboard,
					Representations: []model.Representation{{
						MIME:   "text/plain",
						Inline: []byte("friends can not push this"),
						Hash:   [32]byte{2},
					}},
				}
				return sess.Send(ctx, &wirev1.ControlMessage{
					Body: &wirev1.ControlMessage_ClipAnnounce{
						ClipAnnounce: &wirev1.ClipAnnounce{Item: protoconv.ClipItemToProto(item)},
					},
				})
			},
			check: func(t *testing.T, receiver, _ *testNode) {
				if content, ok := receiver.clipboard.LastWrite(); ok {
					t.Fatalf("the clipboard received %v", content)
				}
			},
		},
		{
			name: "history sync",
			send: func(ctx context.Context, sess *session.Session, _ model.DeviceID) error {
				return sess.Send(ctx, &wirev1.ControlMessage{
					Body: &wirev1.ControlMessage_HistorySync{
						HistorySync: &wirev1.HistorySync{Vector: map[string]uint64{"unknown": 1}},
					},
				})
			},
			check: func(t *testing.T, receiver, _ *testNode) {
				vector, err := receiver.History().Vector(context.Background())
				if err != nil {
					t.Fatalf("read the history vector: %v", err)
				}
				if len(vector) != 0 {
					t.Fatalf("the history took the vector %v", vector)
				}
			},
		},
		{
			name: "history blob request",
			send: func(ctx context.Context, sess *session.Session, _ model.DeviceID) error {
				request, err := sess.OpenRequest(ctx, &wirev1.StreamHeader{
					TransferId:  "forbidden",
					Hash:        hash[:],
					Chunks:      1,
					PayloadType: "blob.request",
				})
				if err != nil {
					return err
				}
				defer func() { _ = request.Close() }()

				// The receiver drops the stream without a handler, so the write
				// or the read may fail; the violation counter shows the refusal.
				if err := request.Write(ctx, &wirev1.BlobRequest{Hash: hash[:], FromChunk: 0, ToChunk: 0, TreeOnly: true}); err != nil {
					return nil
				}
				_ = request.Read(ctx, &wirev1.BlobReply{})
				return nil
			},
			check: func(t *testing.T, receiver, _ *testNode) {
				if _, err := receiver.Blobs().Stat(context.Background(), hash); err == nil {
					t.Fatal("the receiver stored a history blob")
				}
			},
		},
		{
			name: "introduction",
			send: func(ctx context.Context, sess *session.Session, _ model.DeviceID) error {
				return sess.Send(ctx, &wirev1.ControlMessage{
					Body: &wirev1.ControlMessage_Introduction{
						Introduction: &wirev1.Introduction{TsUnixMs: time.Now().UnixMilli()},
					},
				})
			},
			check: func(t *testing.T, receiver, _ *testNode) {
				peers, err := receiver.Trust().List(context.Background(), trust.ListFilter{})
				if err != nil {
					t.Fatalf("read the peers: %v", err)
				}
				if len(peers) != 1 {
					t.Fatalf("the trust store holds %d peers, want one", len(peers))
				}
			},
		},
		{
			name: "revocation",
			send: func(ctx context.Context, sess *session.Session, _ model.DeviceID) error {
				return sess.Send(ctx, &wirev1.ControlMessage{
					Body: &wirev1.ControlMessage_Revocation{
						Revocation: &wirev1.Revocation{TsUnixMs: time.Now().UnixMilli()},
					},
				})
			},
			check: func(t *testing.T, receiver, sender *testNode) {
				peer, err := receiver.Trust().Get(context.Background(), sender.DeviceID())
				if err != nil {
					t.Fatalf("read the peer: %v", err)
				}
				if peer.Revoked {
					t.Fatal("the peer was revoked")
				}
			},
		},
		{
			name: "set peer policy",
			send: func(ctx context.Context, sess *session.Session, _ model.DeviceID) error {
				return sess.Send(ctx, &wirev1.ControlMessage{
					Body: &wirev1.ControlMessage_SetPeerPolicy{
						SetPeerPolicy: &wirev1.SetPeerPolicy{RequireConfirmation: true, TsUnixMs: time.Now().UnixMilli()},
					},
				})
			},
			check: func(t *testing.T, receiver, sender *testNode) {
				peer, err := receiver.Trust().Get(context.Background(), sender.DeviceID())
				if err != nil {
					t.Fatalf("read the peer: %v", err)
				}
				if peer.RequireConfirmation {
					t.Fatal("the peer changed the confirmation flag")
				}
			},
		},
	}

	for i, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			a, b := shareNodes(t, 64000+i*100, model.RoleFriends)
			receiver := peerSession(t, a, b.DeviceID())
			sender := peerSession(t, b, a.DeviceID())
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()

			if err := test.send(ctx, sender, b.DeviceID()); err != nil {
				t.Fatalf("send: %v", err)
			}
			waitViolations(t, receiver, 1)
			test.check(t, a, b)
		})
	}
}

// peerSession returns the live session of the node with the peer.
func peerSession(t *testing.T, n *testNode, peer model.DeviceID) *session.Session {
	t.Helper()

	l := n.lookup(peer)
	if l == nil {
		t.Fatalf("the node has no link to %s", peer)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sess == nil {
		t.Fatalf("the node has no session with %s", peer)
	}
	return l.sess
}

// waitViolations waits until the peer of the session committed a violation.
func waitViolations(t *testing.T, sess *session.Session, want int) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if sess.Violations() >= want {
			return
		}
		select {
		case <-sess.Done():
			t.Fatalf("the session ended before the %dth violation", want)
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatalf("the peer committed %d violations, want %d", sess.Violations(), want)
}
