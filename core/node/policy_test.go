package node

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// waitPeerFlags waits until the flags a node keeps about a peer match want.
// remote selects the flags the peer reported instead of the flags of the node.
func waitPeerFlags(t *testing.T, n *testNode, peer model.DeviceID, remote bool, want trust.Confirmations) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	var got trust.Confirmations
	for time.Now().Before(deadline) {
		record, err := n.Trust().Get(context.Background(), peer)
		if err != nil {
			t.Fatalf("read the peer: %v", err)
		}
		got = record.Confirm
		if remote {
			got = record.ConfirmRemote
		}
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("flags of %s on %s (remote %t) are %+v, want %+v", peer, n.DeviceID(), remote, got, want)
}

// sendPolicy sends a policy of the node to the peer over the live session.
func sendPolicy(t *testing.T, from *testNode, to model.DeviceID, policy trust.SetPeerPolicy) {
	t.Helper()

	l := from.lookup(to)
	if l == nil {
		t.Fatalf("no link to %s", to)
	}
	sess, _, ok := l.onlineSession()
	if !ok {
		t.Fatalf("no session with %s", to)
	}
	message := &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_SetPeerPolicy{SetPeerPolicy: policy.Proto()},
	}
	if err := sess.Send(context.Background(), message); err != nil {
		t.Fatalf("send the policy: %v", err)
	}
}

// TestNodesRequestPeerConfirmation checks that a device asks another one to
// hold its clipboard items, that the other one holds them and that both show
// the same flag.
func TestNodesRequestPeerConfirmation(t *testing.T) {
	a, b := clipNodes(t, 56000)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := a.RequestPeerConfirmation(ctx, b.DeviceID(), trust.ScopeClipboard, true); err != nil {
		t.Fatalf("request the confirmation flag: %v", err)
	}
	waitPeerFlags(t, b, a.DeviceID(), false, trust.Confirmations{Clipboard: true})
	waitPeerFlags(t, a, b.DeviceID(), true, trust.Confirmations{Clipboard: true})
	waitEvent(t, events, EventDeviceUpdated, a.DeviceID())
	if self, err := b.Trust().SelfConfirmations(ctx); err != nil || self.Any() {
		t.Fatalf("the request changed the flags for every device: %+v, %v", self, err)
	}

	a.clipboard.Emit(textSnapshot("remote confirm"))
	event := waitEvent(t, events, EventClipApply, a.DeviceID())
	if _, ok := b.clipboard.LastWrite(); ok {
		t.Fatal("the item reached the clipboard without confirmation")
	}
	if err := b.Clip().ApplyHistory(ctx, event.Item); err != nil {
		t.Fatalf("apply the held item: %v", err)
	}
	waitClipboard(t, b.clipboard, "remote confirm")

	if err := a.RequestPeerConfirmation(ctx, b.DeviceID(), trust.ScopeClipboard, false); err != nil {
		t.Fatalf("clear the confirmation flag: %v", err)
	}
	waitPeerFlags(t, b, a.DeviceID(), false, trust.Confirmations{})
	waitPeerFlags(t, a, b.DeviceID(), true, trust.Confirmations{})
}

// TestNodesSetConfirmationReachesPeer checks that a flag set on the device that
// receives is reported to the other device.
func TestNodesSetConfirmationReachesPeer(t *testing.T) {
	a, b := clipNodes(t, 57000)
	events := a.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.SetConfirmation(ctx, a.DeviceID(), trust.ScopeFiles, true); err != nil {
		t.Fatalf("set the confirmation flag: %v", err)
	}
	waitPeerFlags(t, b, a.DeviceID(), false, trust.Confirmations{Files: true})
	waitPeerFlags(t, a, b.DeviceID(), true, trust.Confirmations{Files: true})
	waitEvent(t, events, EventDeviceUpdated, b.DeviceID())

	// A files flag leaves the clipboard alone.
	a.clipboard.Emit(textSnapshot("not held"))
	waitClipboard(t, b.clipboard, "not held")
}

// TestNodesConfirmationAnnouncedOnOpen checks that a device learns the flags of
// the other one when a session opens.
func TestNodesConfirmationAnnouncedOnOpen(t *testing.T) {
	a, b := clipNodes(t, 57500)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := b.Trust().SetRequireConfirmation(ctx, a.DeviceID(), trust.ScopeClipboard, true); err != nil {
		t.Fatalf("set the flag in the store: %v", err)
	}
	dropSession(t, b, a.DeviceID())
	waitOnline(t, a, b.DeviceID())
	waitPeerFlags(t, a, b.DeviceID(), true, trust.Confirmations{Clipboard: true})
}

// TestNodesSelfConfirmation checks that the flag for every device holds the
// items of every peer.
func TestNodesSelfConfirmation(t *testing.T) {
	a, b := clipNodes(t, 58000)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.SetConfirmation(ctx, b.DeviceID(), trust.ScopeClipboard, true); err != nil {
		t.Fatalf("set the flag for every device: %v", err)
	}

	a.clipboard.Emit(textSnapshot("local confirm"))
	event := waitEvent(t, events, EventClipApply, a.DeviceID())
	if _, ok := b.clipboard.LastWrite(); ok {
		t.Fatal("the item reached the clipboard without confirmation")
	}
	if _, err := b.History().Get(ctx, event.Item.ID); err != nil {
		t.Fatalf("the held item is not in the history: %v", err)
	}
	if err := a.RequestPeerConfirmation(ctx, a.DeviceID(), trust.ScopeClipboard, true); !errors.Is(err, trust.ErrInvalidPeer) {
		t.Fatalf("a request to this device error %v, want %v", err, trust.ErrInvalidPeer)
	}
}

// TestNodesSetPeerPolicyReplay checks that a device ignores an old policy.
func TestNodesSetPeerPolicyReplay(t *testing.T) {
	a, b := clipNodes(t, 60000)

	at := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	newest, err := trust.NewSetPeerPolicy(a.Identity(), b.DeviceID(), trust.ScopeClipboard, true, at)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	sendPolicy(t, a, b.DeviceID(), newest)
	waitPeerFlags(t, b, a.DeviceID(), false, trust.Confirmations{Clipboard: true})

	replay, err := trust.NewSetPeerPolicy(a.Identity(), b.DeviceID(), trust.ScopeClipboard, false, at.Add(-time.Second))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	sendPolicy(t, a, b.DeviceID(), replay)

	// The replay is ordered after the applied policy on the control stream, so
	// it reaches the device well within this window.
	time.Sleep(500 * time.Millisecond)
	peer, err := b.Trust().Get(context.Background(), a.DeviceID())
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if !peer.Confirm.Clipboard {
		t.Fatal("the replay changed the flag")
	}
}

// TestNodesSetPeerPolicyFromFriend checks that a Friends device cannot change
// a flag.
func TestNodesSetPeerPolicyFromFriend(t *testing.T) {
	a, b := nodesWithRole(t, 62000, model.RoleFriends)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := a.RequestPeerConfirmation(ctx, b.DeviceID(), trust.ScopeFiles, true); !errors.Is(err, trust.ErrInvalidPeer) {
		t.Fatalf("local check error %v, want %v", err, trust.ErrInvalidPeer)
	}
	if err := a.SetConfirmation(ctx, b.DeviceID(), trust.ScopeFiles, true); !errors.Is(err, trust.ErrInvalidPeer) {
		t.Fatalf("local flag error %v, want %v", err, trust.ErrInvalidPeer)
	}

	policy, err := trust.NewSetPeerPolicy(a.Identity(), b.DeviceID(), trust.ScopeFiles, true, time.Now())
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	sendPolicy(t, a, b.DeviceID(), policy)

	time.Sleep(500 * time.Millisecond)
	peer, err := b.Trust().Get(context.Background(), a.DeviceID())
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if peer.Confirm.Any() {
		t.Fatal("a friend changed the flag")
	}
}

// nodesWithRole starts two nodes that store each other with the given role.
func nodesWithRole(t *testing.T, base int, role model.Role) (*testNode, *testNode) {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, base)
	b := newTestNode(t, network, bus, base+1000)
	a.start(t)
	b.start(t)
	if role == model.RoleFriends {
		enableFriends(t, a, b)
	}

	for _, pair := range [][2]*testNode{{a, b}, {b, a}} {
		from, to := pair[0], pair[1]
		err := from.AddPeer(context.Background(), trust.Peer{
			Public:   to.Identity().Public(),
			Name:     to.cfg.Name,
			Platform: model.PlatformMacOS,
			Role:     role,
		})
		if err != nil {
			t.Fatalf("add peer: %v", err)
		}
	}
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())
	return a, b
}
