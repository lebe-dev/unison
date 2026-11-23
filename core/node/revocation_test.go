package node

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/discovery"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// waitRevoked waits until the node holds a revoked record of the device.
func waitRevoked(t *testing.T, n *testNode, subject model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		peer, err := n.Trust().Get(context.Background(), subject)
		if err == nil && peer.Revoked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s does not revoke %s within %v", n.DeviceID(), subject, testTimeout)
}

// waitOffline waits until the node has no session with the peer.
func waitOffline(t *testing.T, n *testNode, peer model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if !n.online(peer) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s keeps a session with %s (%s)", n.DeviceID(), peer, n.linkState(peer))
}

// waitDiscoveryKey waits until the node stores the group key.
func waitDiscoveryKey(t *testing.T, n *testNode, want []byte) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		key, err := n.Trust().DiscoveryKey(context.Background())
		if err == nil && bytes.Equal(key, want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not receive the group key within %v", n.DeviceID(), testTimeout)
}

// TestRevokeCutsOffDevice checks a revocation of a device of the group: the
// other devices learn it over gossip, the device is cut off, the discovery key
// is rotated and pairing with the revoked device is refused.
func TestRevokeCutsOffDevice(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 60000)
	b := newTestNode(t, network, bus, 61000)
	c := newTestNode(t, network, bus, 62000)
	for _, n := range []*testNode{a, b, c} {
		n.start(t)
	}

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	// The device joins the group through one device and is introduced to the
	// other one.
	pairThroughWindow(t, c, a)
	waitTrusted(t, b, c.DeviceID())
	waitOnline(t, a, c.DeviceID())
	waitOnline(t, b, c.DeviceID())

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	oldKey, err := c.Trust().DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("read the group key of the device: %v", err)
	}

	if err := a.RevokePeer(ctx, c.DeviceID()); err != nil {
		t.Fatalf("revoke the device: %v", err)
	}

	revoked, err := a.Trust().Get(ctx, c.DeviceID())
	if err != nil {
		t.Fatalf("read the revoked device: %v", err)
	}
	if !revoked.Revoked {
		t.Fatal("the device is not revoked")
	}
	newKey, err := a.Trust().DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("read the group key: %v", err)
	}
	if bytes.Equal(newKey, oldKey) {
		t.Fatal("the group key was not rotated")
	}

	// The other device learns the revocation over gossip and cuts the device
	// off.
	waitRevoked(t, b, c.DeviceID())
	waitOffline(t, a, c.DeviceID())
	waitOffline(t, b, c.DeviceID())
	waitOffline(t, c, a.DeviceID())
	waitOffline(t, c, b.DeviceID())

	// The rotated key reaches the group. The tag of the revoked device does not
	// match it, and the revoked device does not recognise the new tag.
	waitDiscoveryKey(t, b, newKey)
	tag := discovery.Tag(oldKey, time.Now())
	if discovery.MatchTag(newKey, tag, time.Now()) {
		t.Fatal("the group key still matches the tag of the revoked device")
	}
	keys, err := c.Trust().DiscoveryKeys(ctx)
	if err != nil {
		t.Fatalf("read the keys of the revoked device: %v", err)
	}
	if discovery.MatchAnyTag(keys, discovery.Tag(newKey, time.Now()), time.Now()) {
		t.Fatal("the revoked device still recognises the group tag")
	}

	// The revoked device keeps trying, but every connection is refused.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.online(a.DeviceID()) || c.online(b.DeviceID()) {
			t.Fatal("the revoked device connected to the group")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Pairing the revoked device with the same keys is refused.
	if err := a.ClosePairing(); err != nil {
		t.Fatalf("close the pairing window: %v", err)
	}
	if err := c.ClosePairing(); err != nil {
		t.Fatalf("close the pairing window: %v", err)
	}
	eventsA := a.subscribe(t)
	eventsC := c.subscribe(t)
	if err := a.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open the pairing window of the group device: %v", err)
	}
	if err := c.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open the pairing window of the revoked device: %v", err)
	}
	candidate := waitEvent(t, eventsC, EventPairingCandidate, model.DeviceID{})
	if err := c.StartPairing(candidate.Addr); err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	event := waitEvent(t, eventsA, EventPairingFailed, c.DeviceID())
	if !errors.Is(event.Err, pairing.ErrPeerRevoked) {
		t.Fatalf("pairing with the revoked device failed with %v", event.Err)
	}
}

// TestRevokeSelfLeavesGroup checks that a device that revokes itself leaves the
// group and that the other devices revoke it.
func TestRevokeSelfLeavesGroup(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 63000)
	b := newTestNode(t, network, bus, 64000)
	a.start(t)
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := a.RevokeSelf(ctx); err != nil {
		t.Fatalf("revoke itself: %v", err)
	}

	peers, err := a.Trust().List(ctx, trust.ListFilter{IncludeRevoked: true})
	if err != nil {
		t.Fatalf("list the peers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("the device keeps %d peers of the group", len(peers))
	}
	if _, err := a.Trust().DiscoveryKey(ctx); !errors.Is(err, trust.ErrSecretNotFound) {
		t.Fatalf("the device keeps the group key: %v", err)
	}

	waitRevoked(t, b, a.DeviceID())
	waitOffline(t, b, a.DeviceID())
}

// TestRevokeUnknownDevice checks the call for a device that is not paired.
func TestRevokeUnknownDevice(t *testing.T) {
	n := newTestNode(t, vnet.NewNetwork(), fake.NewBus(), 65000)
	n.start(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.RevokePeer(ctx, model.DeviceID{1}); !errors.Is(err, trust.ErrPeerNotFound) {
		t.Fatalf("revoking an unknown device: %v", err)
	}
}
