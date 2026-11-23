package node

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// TestNodesSyncClipboard pairs two nodes, lets discovery find them and moves a
// clipboard item from one to the other.
func TestNodesSyncClipboard(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 20000)
	b := newTestNode(t, network, bus, 21000)
	a.start(t)
	b.start(t)

	eventsA := a.subscribe(t)
	eventsB := b.subscribe(t)

	a.pair(t, b)
	b.pair(t, a)

	waitEvent(t, eventsA, EventPeerOnline, b.DeviceID())
	waitEvent(t, eventsB, EventPeerOnline, a.DeviceID())
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	// The devices found each other through discovery: the side that dialed
	// cached the listener address it discovered, and no address was set by
	// hand.
	small, large := smaller(a, b)
	waitSingleConnection(t, small, large)
	checkCachedAddr(t, small, large)
	// The device dials from its listener socket, so the side that accepted
	// learned an address it can dial back.
	checkCachedAddr(t, large, small)

	a.clipboard.Emit(textSnapshot("hello from a"))
	waitClipboard(t, b.clipboard, "hello from a")

	event := waitEvent(t, eventsB, EventClipboard, a.DeviceID())
	if event.Local {
		t.Fatal("an incoming item is marked as local")
	}
	if event.Item.Origin != a.DeviceID() {
		t.Fatalf("item origin is %s, want %s", event.Item.Origin, a.DeviceID())
	}

	b.clipboard.Emit(textSnapshot("hello from b"))
	waitClipboard(t, a.clipboard, "hello from b")
}

// TestSimultaneousDial checks that both devices dialing each other ends with the
// single connection that the device with the lower device ID initiated.
func TestSimultaneousDial(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 22000)
	b := newTestNode(t, network, bus, 23000)
	a.start(t)
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	// Drop what the pairing left behind and let both nodes dial at once.
	a.closeSessions()
	b.closeSessions()
	dialBoth(t, a, b)

	small, large := smaller(a, b)
	waitSingleConnection(t, small, large)
}

// waitSingleConnection waits until the two nodes share the connection that the
// smaller device initiated and hold it for a moment.
func waitSingleConnection(t *testing.T, small, large *testNode) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for {
		if !singleConnection(small, large) {
			if time.Now().After(deadline) {
				smallLocal, smallRemote := small.sessionAddrs(large.DeviceID())
				largeLocal, largeRemote := large.sessionAddrs(small.DeviceID())
				t.Fatalf("the nodes did not settle on one connection: small %s -> %s, large %s -> %s",
					smallLocal, smallRemote, largeLocal, largeRemote)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// The connection has to hold: a replacement in the meantime means the
		// nodes did not settle.
		time.Sleep(300 * time.Millisecond)
		if singleConnection(small, large) {
			return
		}
	}
}

// singleConnection reports whether the nodes share the connection the smaller
// device initiated.
func singleConnection(small, large *testNode) bool {
	smallLocal, smallRemote := small.sessionAddrs(large.DeviceID())
	largeLocal, largeRemote := large.sessionAddrs(small.DeviceID())
	return smallLocal == small.listenAddr() && smallRemote == large.listenAddr() &&
		largeLocal == large.listenAddr() && largeRemote == small.listenAddr()
}

// TestUnknownDeviceRefused checks that a device that is not in the trust store
// is refused during the handshake and does not surface as an event.
func TestUnknownDeviceRefused(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 26000)
	a.start(t)
	events := a.subscribe(t)

	stranger, err := identity.FromSeeds(bytes.Repeat([]byte{7}, identity.SeedSize), bytes.Repeat([]byte{9}, identity.SeedSize))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	pc, err := network.ListenPacket("127.0.0.1:26010")
	if err != nil {
		t.Fatalf("packet connection: %v", err)
	}
	dialer, err := transport.NewDialer(pc, stranger, acceptPeer{peer: transport.Peer{
		Public: a.Identity().Public(),
		Role:   model.RolePersonal,
	}}, transport.Config{HandshakeTimeout: time.Second})
	if err != nil {
		_ = pc.Close()
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := dialer.Dial(ctx, vnet.NewAddr(a.listenAddr())); err == nil {
		t.Fatal("the unknown device was accepted")
	}

	select {
	case event := <-events.Events():
		t.Fatalf("the unknown device produced the %s event", event.Kind)
	case <-time.After(200 * time.Millisecond):
	}
}

// acceptPeer accepts any peer certificate and returns a fixed peer.
type acceptPeer struct {
	peer transport.Peer
}

// VerifyPeer returns the fixed peer.
func (v acceptPeer) VerifyPeer(context.Context, *x509.Certificate) (transport.Peer, error) {
	return v.peer, nil
}

// TestLifecycle checks that a node starts once and stops more than once.
func TestLifecycle(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	n := newTestNode(t, network, bus, 27000)
	n.start(t)

	if n.DeviceID().IsZero() {
		t.Fatal("the node has no device ID")
	}
	if n.Addr() == nil {
		t.Fatal("the node has no listener address")
	}
	if err := n.Start(context.Background()); !errors.Is(err, ErrStarted) {
		t.Fatalf("second start returns %v, want %v", err, ErrStarted)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if err := n.Start(ctx); !errors.Is(err, ErrStopped) {
		t.Fatalf("start after stop returns %v, want %v", err, ErrStopped)
	}
	if _, err := n.Peers(ctx); !errors.Is(err, ErrStopped) {
		t.Fatalf("peers after stop returns %v, want %v", err, ErrStopped)
	}
}

// TestNodesTransferBlob checks that the blob store and the transfer service of
// two nodes work on one pair of devices.
func TestNodesTransferBlob(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 28000)
	b := newTestNode(t, network, bus, 29000)
	a.start(t)
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())
	small, large := smaller(a, b)
	waitSingleConnection(t, small, large)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	content := []byte("a blob from the other device")
	info, err := a.Blobs().Put(ctx, bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}

	peers := waitBlobPeers(t, b, 1)
	if _, err := b.Fetcher().Fetch(ctx, transfer.FetchRequest{Hash: info.Hash, Peers: peers}); err != nil {
		t.Fatalf("fetch blob: %v", err)
	}

	reader, err := b.Blobs().Open(ctx, info.Hash)
	if err != nil {
		t.Fatalf("open blob: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	got := make([]byte, reader.Size())
	if _, err := reader.ReadAt(got, 0); err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("the fetched blob is %q, want %q", got, content)
	}
}

// waitBlobPeers waits until the node reports the wanted number of blob peers.
func waitBlobPeers(t *testing.T, n *testNode, want int) []transfer.Peer {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if peers := n.BlobPeers(); len(peers) == want {
			return peers
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the node reports %d blob peers within %v", want, testTimeout)
	return nil
}

// TestPolicyDecidesIncomingText checks that the clipboard policy of the node
// decides about an announced item.
func TestPolicyDecidesIncomingText(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 30000)
	b := newTestNode(t, network, bus, 31000)
	a.start(t)
	b.start(t)

	events := a.subscribe(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())
	small, large := smaller(a, b)
	waitSingleConnection(t, small, large)

	config := a.Policy().Config()
	config.Unmetered.Text.Action = policy.ActionMetadataOnly
	a.Policy().SetConfig(config)

	b.clipboard.Emit(textSnapshot("this item is kept"))
	waitEvent(t, events, EventClipDownload, b.DeviceID())
	waitHistoryText(t, a, "this item is kept")
	if content, ok := a.clipboard.LastWrite(); ok {
		t.Fatalf("a metadata item reached the clipboard: %v", content)
	}

	config = a.Policy().Config()
	config.Unmetered.Text.Action = policy.ActionReject
	a.Policy().SetConfig(config)

	b.clipboard.Emit(textSnapshot("this item is refused"))
	time.Sleep(time.Second)
	if content, ok := a.clipboard.LastWrite(); ok {
		t.Fatalf("the refused item reached the clipboard: %v", content)
	}
	for _, text := range historyTexts(t, a) {
		if text == "this item is refused" {
			t.Fatal("the refused item reached the history")
		}
	}
}

// TestPairingWindowPairs checks that two nodes pair through the windows the node
// owns and connect afterwards.
func TestPairingWindowPairs(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 32000)
	b := newTestNode(t, network, bus, 33000)
	a.start(t)
	b.start(t)

	eventsA := a.subscribe(t)
	eventsB := b.subscribe(t)

	if err := a.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open the window of the first node: %v", err)
	}
	if err := b.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open the window of the second node: %v", err)
	}

	candidate := waitEvent(t, eventsA, EventPairingCandidate, model.DeviceID{})
	if err := a.StartPairing(candidate.Addr); err != nil {
		t.Fatalf("start pairing: %v", err)
	}

	requestA := waitEvent(t, eventsA, EventPairingRequest, model.DeviceID{})
	requestB := waitEvent(t, eventsB, EventPairingRequest, model.DeviceID{})
	if requestA.SAS.String() != requestB.SAS.String() {
		t.Fatalf("the SAS differs: %s and %s", requestA.SAS, requestB.SAS)
	}
	if requestA.Role != model.RolePersonal {
		t.Fatalf("the proposed role is %s", requestA.Role)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := a.ConfirmPairing(ctx, requestA.Peer); err != nil {
		t.Fatalf("confirm on the first node: %v", err)
	}
	if err := b.ConfirmPairing(ctx, requestB.Peer); err != nil {
		t.Fatalf("confirm on the second node: %v", err)
	}

	waitEvent(t, eventsA, EventPairingPaired, model.DeviceID{})
	waitEvent(t, eventsB, EventPairingPaired, model.DeviceID{})
	waitOnline(t, a, requestA.Peer)
	waitOnline(t, b, requestB.Peer)
}

// TestReconnectAfterDisconnect checks that a peer that lost its session comes
// back with a new one.
func TestReconnectAfterDisconnect(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 24000)
	b := newTestNode(t, network, bus, 25000)
	a.start(t)
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	beforeA := a.generation(b.DeviceID())
	beforeB := b.generation(a.DeviceID())
	if beforeA == 0 || beforeB == 0 {
		t.Fatal("the peers are not online before the disconnect")
	}

	a.closeSessions()
	waitNewSession(t, a, b.DeviceID(), beforeA)
	waitNewSession(t, b, a.DeviceID(), beforeB)
	waitPeerState(t, a, b.DeviceID())
	waitPeerState(t, b, a.DeviceID())
}

// waitPeerState waits until the node reports the peer as online.
func waitPeerState(t *testing.T, n *testNode, peer model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		state, err := n.Peer(context.Background(), peer)
		if err != nil {
			t.Fatalf("read peer state: %v", err)
		}
		if state.Online && state.Path == model.PathLAN {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the peer %s is not reported online on the LAN path", peer)
}
