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

// waitConfirmation waits until the local confirmation flag of the node has the
// wanted state.
func waitConfirmation(t *testing.T, n *testNode, want bool) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		require, err := n.Trust().SelfRequireConfirmation(context.Background())
		if err != nil {
			t.Fatalf("read the confirmation flag: %v", err)
		}
		if require == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the confirmation flag of %s is not %t within %v", n.DeviceID(), want, testTimeout)
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

// TestNodesSetPeerPolicy checks that a peer turns on the confirmation flag of
// another device and that the device then holds announcements.
func TestNodesSetPeerPolicy(t *testing.T) {
	a, b := clipNodes(t, 56000)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := a.SetPeerRequireConfirmation(ctx, b.DeviceID(), true); err != nil {
		t.Fatalf("set the remote confirmation flag: %v", err)
	}
	waitConfirmation(t, b, true)

	a.clipboard.Emit(textSnapshot("remote confirm"))
	event := waitEvent(t, events, EventClipApply, a.DeviceID())
	if _, ok := b.clipboard.LastWrite(); ok {
		t.Fatal("the item reached the clipboard without confirmation")
	}

	if err := b.Clip().ApplyHistory(ctx, event.Item); err != nil {
		t.Fatalf("apply the held item: %v", err)
	}
	waitClipboard(t, b.clipboard, "remote confirm")
}

// TestNodesSetPeerPolicyLocalFlag checks that the local flag holds the items of
// every peer.
func TestNodesSetPeerPolicyLocalFlag(t *testing.T) {
	a, b := clipNodes(t, 58000)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.SetPeerRequireConfirmation(ctx, b.DeviceID(), true); err != nil {
		t.Fatalf("set the local confirmation flag: %v", err)
	}

	a.clipboard.Emit(textSnapshot("local confirm"))
	event := waitEvent(t, events, EventClipApply, a.DeviceID())
	if _, ok := b.clipboard.LastWrite(); ok {
		t.Fatal("the item reached the clipboard without confirmation")
	}
	if _, err := b.History().Get(ctx, event.Item.ID); err != nil {
		t.Fatalf("the held item is not in the history: %v", err)
	}
}

// TestNodesSetPeerPolicyReplay checks that a device ignores an old policy.
func TestNodesSetPeerPolicyReplay(t *testing.T) {
	a, b := clipNodes(t, 60000)

	at := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	newest, err := trust.NewSetPeerPolicy(a.Identity(), b.DeviceID(), true, at)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	sendPolicy(t, a, b.DeviceID(), newest)
	waitConfirmation(t, b, true)

	replay, err := trust.NewSetPeerPolicy(a.Identity(), b.DeviceID(), false, at.Add(-time.Second))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	sendPolicy(t, a, b.DeviceID(), replay)

	// The replay is ordered after the applied policy on the control stream, so
	// it reaches the device well within this window.
	time.Sleep(500 * time.Millisecond)
	require, err := b.Trust().SelfRequireConfirmation(context.Background())
	if err != nil {
		t.Fatalf("read the confirmation flag: %v", err)
	}
	if !require {
		t.Fatal("the replay changed the flag")
	}
}

// TestNodesSetPeerPolicyFromFriend checks that a Friends device cannot change
// the flag.
func TestNodesSetPeerPolicyFromFriend(t *testing.T) {
	a, b := nodesWithRole(t, 62000, model.RoleFriends)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := a.SetPeerRequireConfirmation(ctx, b.DeviceID(), true); !errors.Is(err, trust.ErrInvalidPeer) {
		t.Fatalf("local check error %v, want %v", err, trust.ErrInvalidPeer)
	}

	policy, err := trust.NewSetPeerPolicy(a.Identity(), b.DeviceID(), true, time.Now())
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	sendPolicy(t, a, b.DeviceID(), policy)

	time.Sleep(500 * time.Millisecond)
	require, err := b.Trust().SelfRequireConfirmation(context.Background())
	if err != nil {
		t.Fatalf("read the confirmation flag: %v", err)
	}
	if require {
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
