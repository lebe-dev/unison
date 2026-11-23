package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// settleTime is how long a test watches a device that has to stay offline.
const settleTime = 500 * time.Millisecond

// TestSetPeerAddress connects two paired devices without discovery: the address
// of the peer is entered by the user.
func TestSetPeerAddress(t *testing.T) {
	network := vnet.NewNetwork()
	one := newNode(t, network, fake.NewBus(), 3300)
	two := newNode(t, network, fake.NewBus(), 3310)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	addr := two.Addr().String()
	client := newClient(t, one.Node, api.Options{DeviceName: one.name})
	response, err := client.SetPeerAddress(callTimeout(t), connect.NewRequest(&localv1.SetPeerAddressRequest{
		DeviceId: two.DeviceID().String(),
		Addr:     addr,
	}))
	if err != nil {
		t.Fatalf("set the address: %v", err)
	}
	if got := response.Msg.GetDevice().GetDeviceId(); got != two.DeviceID().String() {
		t.Errorf("device = %s, want %s", got, two.DeviceID())
	}
	if got := response.Msg.GetDevice().GetName(); got != two.name {
		t.Errorf("name = %q, want %q", got, two.name)
	}

	waitPeerOnline(t, one, two.DeviceID())
	waitPeerOnline(t, two, one.DeviceID())

	peer, err := one.Trust().Get(t.Context(), two.DeviceID())
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if len(peer.LastAddrs) == 0 || peer.LastAddrs[0] != addr {
		t.Errorf("the address cache is %v, want %s first", peer.LastAddrs, addr)
	}
}

// TestSetPeerAddressAfterRestart reconnects devices that were paired before a
// restart, with discovery still unavailable.
func TestSetPeerAddressAfterRestart(t *testing.T) {
	network := vnet.NewNetwork()
	oneBus, twoBus := fake.NewBus(), fake.NewBus()
	oneKeys, twoKeys := fake.NewKeystore(), fake.NewKeystore()
	oneDir, twoDir := t.TempDir(), t.TempDir()

	one := newNodeIn(t, network, oneBus, 3320, oneDir, oneKeys)
	two := newNodeIn(t, network, twoBus, 3330, twoDir, twoKeys)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	oneID, twoID := one.DeviceID(), two.DeviceID()
	addr := two.Addr().String()
	stopNode(t, one)
	stopNode(t, two)

	one = newNodeIn(t, network, oneBus, 3320, oneDir, oneKeys)
	two = newNodeIn(t, network, twoBus, 3330, twoDir, twoKeys)
	one.start(t)
	two.start(t)
	requirePeerOffline(t, one, twoID)
	requirePeerOffline(t, two, oneID)

	client := newClient(t, one.Node, api.Options{DeviceName: one.name})
	if _, err := client.SetPeerAddress(callTimeout(t), connect.NewRequest(&localv1.SetPeerAddressRequest{
		DeviceId: twoID.String(),
		Addr:     addr,
	})); err != nil {
		t.Fatalf("set the address: %v", err)
	}
	waitPeerOnline(t, one, twoID)
	waitPeerOnline(t, two, oneID)
}

// TestSetPeerAddressInvalid checks the refused addresses and devices.
func TestSetPeerAddressInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3340)
	n.start(t)
	other := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3350)
	other.start(t)
	n.pair(t, other)

	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)
	for _, addr := range []string{"", "localhost", "127.0.0.1", "127.0.0.1:", "127.0.0.1:0",
		"127.0.0.1:70000", "::1:5000", strings.Repeat("a", 300)} {
		_, err := client.SetPeerAddress(ctx, connect.NewRequest(&localv1.SetPeerAddressRequest{
			DeviceId: other.DeviceID().String(),
			Addr:     addr,
		}))
		if err == nil {
			t.Errorf("address %q was accepted", addr)
			continue
		}
		requireCode(t, err, connect.CodeInvalidArgument)
	}

	_, err := client.SetPeerAddress(ctx, connect.NewRequest(&localv1.SetPeerAddressRequest{
		DeviceId: "no-hex",
		Addr:     "127.0.0.1:5000",
	}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.SetPeerAddress(ctx, connect.NewRequest(&localv1.SetPeerAddressRequest{
		DeviceId: model.DeviceID{9}.String(),
		Addr:     "127.0.0.1:5000",
	}))
	requireCode(t, err, connect.CodeNotFound)

	revoked := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3380)
	revoked.start(t)
	n.pair(t, revoked)
	if err := n.Trust().MarkRevoked(t.Context(), revoked.DeviceID()); err != nil {
		t.Fatalf("revoke the device: %v", err)
	}
	_, err = client.SetPeerAddress(ctx, connect.NewRequest(&localv1.SetPeerAddressRequest{
		DeviceId: revoked.DeviceID().String(),
		Addr:     "127.0.0.1:5000",
	}))
	requireCode(t, err, connect.CodePermissionDenied)
}

// TestSetPeerAddressBrackets checks an IPv6 address with a zone.
func TestSetPeerAddressBrackets(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3360)
	n.start(t)
	other := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3370)
	other.start(t)
	n.pair(t, other)

	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	_, err := client.SetPeerAddress(callTimeout(t), connect.NewRequest(&localv1.SetPeerAddressRequest{
		DeviceId: other.DeviceID().String(),
		Addr:     "[::1]:5000",
	}))
	if err != nil {
		t.Fatalf("set the address: %v", err)
	}

	peer, err := n.Trust().Get(t.Context(), other.DeviceID())
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if len(peer.LastAddrs) == 0 || peer.LastAddrs[0] != "[::1]:5000" {
		t.Errorf("the address cache is %v, want the IPv6 address first", peer.LastAddrs)
	}
}

// TestRetryDevice connects two paired devices without discovery: the retry
// dials the cached address of the peer.
func TestRetryDevice(t *testing.T) {
	network := vnet.NewNetwork()
	one := newNode(t, network, fake.NewBus(), 3400)
	two := newNode(t, network, fake.NewBus(), 3410)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	// The devices share no discovery, so the cached address is the only way
	// to reach the peer.
	ctx := callTimeout(t)
	if err := one.Trust().UpdateAddrs(ctx, two.DeviceID(), []string{two.Addr().String()}); err != nil {
		t.Fatalf("store the address: %v", err)
	}

	client := newClient(t, one.Node, api.Options{DeviceName: one.name})
	if _, err := client.RetryDevice(ctx, connect.NewRequest(&localv1.RetryDeviceRequest{
		DeviceId: two.DeviceID().String(),
	})); err != nil {
		t.Fatalf("retry the device: %v", err)
	}
	waitPeerOnline(t, one, two.DeviceID())
	waitPeerOnline(t, two, one.DeviceID())
}

// TestRetryDeviceInvalid checks the refused device and the retry of every
// device.
func TestRetryDeviceInvalid(t *testing.T) {
	network := vnet.NewNetwork()
	n := newNode(t, network, fake.NewBus(), 3420)
	revoked := newNode(t, network, fake.NewBus(), 3430)
	n.start(t)
	revoked.start(t)
	n.pair(t, revoked)
	if err := n.Trust().MarkRevoked(t.Context(), revoked.DeviceID()); err != nil {
		t.Fatalf("revoke the device: %v", err)
	}

	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)
	_, err := client.RetryDevice(ctx, connect.NewRequest(&localv1.RetryDeviceRequest{DeviceId: "no-hex"}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.RetryDevice(ctx, connect.NewRequest(&localv1.RetryDeviceRequest{
		DeviceId: model.DeviceID{9}.String(),
	}))
	requireCode(t, err, connect.CodeNotFound)

	_, err = client.RetryDevice(ctx, connect.NewRequest(&localv1.RetryDeviceRequest{
		DeviceId: revoked.DeviceID().String(),
	}))
	requireCode(t, err, connect.CodePermissionDenied)

	if _, err := client.RetryDevice(ctx, connect.NewRequest(&localv1.RetryDeviceRequest{})); err != nil {
		t.Fatalf("retry every device: %v", err)
	}
}

// stopNode stops a node before the test creates it again.
func stopNode(t *testing.T, n *testNode) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("stop node: %v", err)
	}
}

// waitPeerOnline waits until the node has a session with the device.
func waitPeerOnline(t *testing.T, n *testNode, id model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		state, err := n.Peer(t.Context(), id)
		if err != nil {
			t.Fatalf("read the peer: %v", err)
		}
		if state.Online {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no session with %s within %v", id, testTimeout)
}

// requirePeerOffline checks that the device has no session, so a later
// connection is the work of the test.
func requirePeerOffline(t *testing.T, n *testNode, id model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(settleTime)
	for time.Now().Before(deadline) {
		state, err := n.Peer(t.Context(), id)
		if err != nil {
			t.Fatalf("read the peer: %v", err)
		}
		if state.Online {
			t.Fatalf("the node opened a session with %s on its own", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
