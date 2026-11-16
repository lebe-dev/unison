package pairing

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testNetwork returns a virtual network and a discovery bus with its cleanup.
func testNetwork(t *testing.T) (*vnet.Network, *fake.Bus) {
	t.Helper()
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	return network, fake.NewBus()
}

// TestWindowPairsTwoDevices checks the successful SAS flow: both devices show
// the same SAS, both confirm and both write the peer and the group secrets.
func TestWindowPairsTwoDevices(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	alice.open(t, ports.SystemClock, model.RolePersonal)
	bob.open(t, ports.SystemClock, model.RolePersonal)

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	requestA := waitEvent(t, alice.window, EventRequest)
	requestB := waitEvent(t, bob.window, EventRequest)

	if got, want := requestA.DeviceID, bob.deviceID(); got != want {
		t.Errorf("initiator peer = %s, want %s", got, want)
	}
	if got, want := requestB.DeviceID, alice.deviceID(); got != want {
		t.Errorf("responder peer = %s, want %s", got, want)
	}
	if got, want := requestA.Role, model.RolePersonal; got != want {
		t.Errorf("proposed role = %s, want %s", got, want)
	}
	if got, want := requestB.Role, model.RolePersonal; got != want {
		t.Errorf("confirmed role = %s, want %s", got, want)
	}
	if requestA.SAS.String() != requestB.SAS.String() {
		t.Fatalf("SAS differs: %q and %q", requestA.SAS, requestB.SAS)
	}
	if requestA.SAS.IsZero() {
		t.Fatal("SAS is empty")
	}
	if requestA.Name != bob.name {
		t.Errorf("initiator sees name %q, want %q", requestA.Name, bob.name)
	}
	if requestA.Platform != model.PlatformMacOS {
		t.Errorf("initiator sees platform %s, want macos", requestA.Platform)
	}

	ctx := context.Background()
	if err := alice.window.Confirm(ctx, bob.deviceID()); err != nil {
		t.Fatalf("confirm on the initiator: %v", err)
	}
	if err := bob.window.Confirm(ctx, alice.deviceID()); err != nil {
		t.Fatalf("confirm on the responder: %v", err)
	}

	waitEvent(t, alice.window, EventPaired)
	waitEvent(t, bob.window, EventPaired)
	if err := <-pairing; err != nil {
		t.Fatalf("pairing: %v", err)
	}

	assertPeer(t, alice.trust, bob, model.RolePersonal)
	assertPeer(t, bob.trust, alice, model.RolePersonal)
	assertGroupKeys(t, alice, bob)
}

// TestWindowFriendsPairing checks that a Friends pair shares no discovery key.
func TestWindowFriendsPairing(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	alice.open(t, ports.SystemClock, model.RoleFriends)
	bob.open(t, ports.SystemClock, model.RolePersonal)

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	requestB := waitEvent(t, bob.window, EventRequest)
	if got, want := requestB.Role, model.RoleFriends; got != want {
		t.Fatalf("confirmed role = %s, want %s", got, want)
	}
	waitEvent(t, alice.window, EventRequest)

	ctx := context.Background()
	if err := alice.window.Confirm(ctx, bob.deviceID()); err != nil {
		t.Fatalf("confirm on the initiator: %v", err)
	}
	if err := bob.window.Confirm(ctx, alice.deviceID()); err != nil {
		t.Fatalf("confirm on the responder: %v", err)
	}
	if err := <-pairing; err != nil {
		t.Fatalf("pairing: %v", err)
	}

	assertPeer(t, alice.trust, bob, model.RoleFriends)
	assertPeer(t, bob.trust, alice, model.RoleFriends)
	for _, node := range []*testNode{alice, bob} {
		if key, err := node.trust.DiscoveryKey(ctx); !errors.Is(err, trust.ErrSecretNotFound) {
			t.Fatalf("%s has discovery key %x, want none", node.name, key)
		}
	}
}

// TestWindowRejectedByResponder checks that a refusal on either side writes
// nothing on either device.
func TestWindowRejectedByResponder(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	alice.open(t, ports.SystemClock, model.RolePersonal)
	bob.open(t, ports.SystemClock, model.RolePersonal)

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	waitEvent(t, alice.window, EventRequest)
	waitEvent(t, bob.window, EventRequest)

	ctx := context.Background()
	if err := alice.window.Confirm(ctx, bob.deviceID()); err != nil {
		t.Fatalf("confirm on the initiator: %v", err)
	}
	if err := bob.window.Reject(ctx, alice.deviceID()); err != nil {
		t.Fatalf("reject on the responder: %v", err)
	}

	if err := <-pairing; !errors.Is(err, ErrRejected) {
		t.Fatalf("pairing error = %v, want %v", err, ErrRejected)
	}
	failure := waitEvent(t, alice.window, EventFailed)
	if !errors.Is(failure.Err, ErrRejected) {
		t.Errorf("failure error = %v, want %v", failure.Err, ErrRejected)
	}
	if got, want := failure.DeviceID, bob.deviceID(); got != want {
		t.Errorf("failed device = %s, want %s", got, want)
	}
	waitEvent(t, bob.window, EventFailed)

	assertNoPeer(t, alice.trust, bob.deviceID())
	assertNoPeer(t, bob.trust, alice.deviceID())
}

// TestWindowRejectedByInitiator checks the other side of a refusal: the device
// that started the exchange refuses.
func TestWindowRejectedByInitiator(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	alice.open(t, ports.SystemClock, model.RolePersonal)
	bob.open(t, ports.SystemClock, model.RolePersonal)

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	waitEvent(t, alice.window, EventRequest)
	waitEvent(t, bob.window, EventRequest)

	ctx := context.Background()
	if err := alice.window.Reject(ctx, bob.deviceID()); err != nil {
		t.Fatalf("reject on the initiator: %v", err)
	}
	if err := <-pairing; !errors.Is(err, ErrRejected) {
		t.Fatalf("pairing error = %v, want %v", err, ErrRejected)
	}
	failure := waitEvent(t, bob.window, EventFailed)
	if !errors.Is(failure.Err, ErrRejected) {
		t.Errorf("failure error = %v, want %v", failure.Err, ErrRejected)
	}

	assertNoPeer(t, alice.trust, bob.deviceID())
	assertNoPeer(t, bob.trust, alice.deviceID())
}

// TestWindowTimeout checks that the window closes by itself and that an
// unfinished request writes nothing.
func TestWindowTimeout(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)

	clock := fake.NewClock(time.Now())
	bob.open(t, clock, model.RolePersonal)
	alice.open(t, ports.SystemClock, model.RolePersonal)

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	waitEvent(t, alice.window, EventRequest)
	waitEvent(t, bob.window, EventRequest)

	clock.Advance(DefaultWindow)
	waitDone(t, bob.window)

	if err := <-pairing; err == nil {
		t.Fatal("the pairing succeeded on a closed window")
	}
	assertNoPeer(t, bob.trust, alice.deviceID())
	assertNoPeer(t, alice.trust, bob.deviceID())

	// The listener is gone with the window.
	dialer, closeDialer := newTestDialer(t, network, "127.0.0.1:43000")
	defer closeDialer()
	result := make(chan error, 1)
	go func() {
		conn, err := dialer.DialPairing(context.Background(), vnet.NewAddr(addr), identity.CertFingerprint{})
		if err == nil {
			_ = conn.Close()
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("the pairing listener still accepts connections")
		}
	case <-time.After(testTimeout):
		t.Fatal("dialing the closed window did not fail")
	}
}

// TestWindowExtend checks that the window stays open after an extension.
func TestWindowExtend(t *testing.T) {
	network, bus := testNetwork(t)
	node := newNode(t, network, bus, 1, 41000)

	start := time.Now()
	clock := fake.NewClock(start)
	node.open(t, clock, model.RolePersonal)
	window := node.window

	if want := start.Add(DefaultWindow); !window.Deadline().Equal(want) {
		t.Fatalf("deadline = %v, want %v", window.Deadline(), want)
	}

	clock.Advance(2 * time.Minute)
	select {
	case <-window.Done():
		t.Fatal("the window closed before its time")
	default:
	}

	if err := window.Extend(); err != nil {
		t.Fatalf("extend: %v", err)
	}
	if want := start.Add(2*time.Minute + DefaultWindow); !window.Deadline().Equal(want) {
		t.Fatalf("deadline after the extension = %v, want %v", window.Deadline(), want)
	}

	clock.Advance(2 * time.Minute)
	select {
	case <-window.Done():
		t.Fatal("the window closed after the extension")
	default:
	}

	clock.Advance(DefaultWindow)
	waitDone(t, window)
}

// TestWindowRepairsKnownDevice checks that pairing a device that is already
// trusted again succeeds and updates its display name.
func TestWindowRepairsKnownDevice(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)

	pairOnce(t, alice, bob)
	closeWindows(t, alice, bob)
	bob.name = "renamed-device"
	pairOnce(t, alice, bob)

	peer, err := alice.trust.Get(context.Background(), bob.deviceID())
	if err != nil {
		t.Fatalf("read peer: %v", err)
	}
	if got, want := peer.Name, "renamed-device"; got != want {
		t.Fatalf("peer name = %q, want %q", got, want)
	}
	peers, err := alice.trust.List(context.Background(), trust.ListFilter{})
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("the initiator has %d peers, want 1", len(peers))
	}
}

// TestWindowRepairWithAnotherRole checks that an already paired device cannot
// change its role by pairing again.
func TestWindowRepairWithAnotherRole(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)

	pairOnce(t, alice, bob)
	closeWindows(t, alice, bob)
	alice.open(t, ports.SystemClock, model.RoleFriends)
	bob.open(t, ports.SystemClock, model.RolePersonal)

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	if err := <-pairing; !errors.Is(err, ErrRoleMismatch) {
		t.Fatalf("pairing error = %v, want %v", err, ErrRoleMismatch)
	}
	failure := waitEvent(t, bob.window, EventFailed)
	if !errors.Is(failure.Err, ErrRoleMismatch) {
		t.Errorf("failure error = %v, want %v", failure.Err, ErrRoleMismatch)
	}

	assertPeer(t, alice.trust, bob, model.RolePersonal)
	assertPeer(t, bob.trust, alice, model.RolePersonal)
}

// TestWindowRevokedResponderRefuses checks that a device the responder revoked
// learns that the pair was refused instead of losing the stream.
func TestWindowRevokedResponderRefuses(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)

	pairOnce(t, alice, bob)
	closeWindows(t, alice, bob)
	if err := alice.trust.MarkRevoked(context.Background(), bob.deviceID()); err != nil {
		t.Fatalf("revoke the device: %v", err)
	}

	alice.open(t, ports.SystemClock, model.RolePersonal)
	bob.open(t, ports.SystemClock, model.RolePersonal)
	addr := alice.window.Addr().String()
	waitCandidate(t, bob.window, addr)

	pairing := dial(bob.window, addr)
	if err := <-pairing; !errors.Is(err, ErrRejected) {
		t.Fatalf("pairing error = %v, want %v", err, ErrRejected)
	}
	failure := waitEvent(t, alice.window, EventFailed)
	if !errors.Is(failure.Err, ErrPeerRevoked) {
		t.Errorf("failure error = %v, want %v", failure.Err, ErrPeerRevoked)
	}
}

// TestWindowDialAndConfirmErrors checks the errors of an unknown request and of
// a closed window.
func TestWindowDialAndConfirmErrors(t *testing.T) {
	network, bus := testNetwork(t)
	node := newNode(t, network, bus, 1, 41000)
	node.open(t, ports.SystemClock, model.RolePersonal)

	ctx := context.Background()
	if err := node.window.Confirm(ctx, node.deviceID()); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("confirm error = %v, want %v", err, ErrUnknownRequest)
	}
	if err := node.window.Dial(ctx, ""); err == nil {
		t.Error("an empty address was accepted")
	}
	if err := node.window.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := node.window.Extend(); !errors.Is(err, ErrClosed) {
		t.Errorf("extend error = %v, want %v", err, ErrClosed)
	}
	if err := node.window.Dial(ctx, "127.0.0.1:9"); !errors.Is(err, ErrClosed) {
		t.Errorf("dial error = %v, want %v", err, ErrClosed)
	}
}

// TestWindowRefusesTokenRequest checks that a QR request with a token does not
// open a SAS exchange.
func TestWindowRefusesTokenRequest(t *testing.T) {
	network, bus := testNetwork(t)
	node := newNode(t, network, bus, 1, 41000)
	node.open(t, ports.SystemClock, model.RolePersonal)

	client := testIdentity(t, 9)
	dialer, closeDialer := newTestDialer(t, network, "127.0.0.1:43000")
	defer closeDialer()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := dialer.DialPairing(ctx, vnet.NewAddr(node.window.Addr().String()), identity.CertFingerprint{})
	if err != nil {
		t.Fatalf("dial pairing: %v", err)
	}
	defer func() { _ = conn.Close() }()

	stream, err := conn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	public := client.Public()
	request := &wirev1.PairRequest{
		Token:      bytes.Repeat([]byte{1}, 16),
		Ed25519Pub: public.Ed25519,
		MldsaPub:   public.MLDSABytes(),
		DeviceName: "stranger",
		Platform:   wirev1.Platform_PLATFORM_MACOS,
		Role:       wirev1.Role_ROLE_PERSONAL,
	}
	if err := writeMessage(stream, request); err != nil {
		t.Fatalf("send request: %v", err)
	}

	_ = stream.SetReadDeadline(time.Now().Add(testTimeout))
	buffer := make([]byte, 1)
	if _, err := stream.Read(buffer); err == nil {
		t.Fatal("the window answered a request with a token")
	}
	assertNoPeer(t, node.trust, client.DeviceID())
}

// pairOnce pairs two nodes that already have open windows and checks the
// result.
func pairOnce(t *testing.T, alice, bob *testNode) {
	t.Helper()
	if alice.window == nil {
		alice.open(t, ports.SystemClock, model.RolePersonal)
	}
	if bob.window == nil {
		bob.open(t, ports.SystemClock, model.RolePersonal)
	}

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	waitEvent(t, alice.window, EventRequest)
	waitEvent(t, bob.window, EventRequest)

	ctx := context.Background()
	if err := alice.window.Confirm(ctx, bob.deviceID()); err != nil {
		t.Fatalf("confirm on the initiator: %v", err)
	}
	if err := bob.window.Confirm(ctx, alice.deviceID()); err != nil {
		t.Fatalf("confirm on the responder: %v", err)
	}
	waitEvent(t, alice.window, EventPaired)
	waitEvent(t, bob.window, EventPaired)
	if err := <-pairing; err != nil {
		t.Fatalf("pairing: %v", err)
	}
}

// closeWindows closes the windows of the nodes.
func closeWindows(t *testing.T, nodes ...*testNode) {
	t.Helper()
	for _, node := range nodes {
		if err := node.window.Close(); err != nil {
			t.Fatalf("close window of %s: %v", node.name, err)
		}
		node.window = nil
	}
}

// assertPeer checks the stored record of a peer.
func assertPeer(t *testing.T, trustStore *trust.Store, node *testNode, role model.Role) {
	t.Helper()
	peer, err := trustStore.Get(context.Background(), node.deviceID())
	if err != nil {
		t.Fatalf("read peer %s: %v", node.name, err)
	}
	if peer.Role != role {
		t.Errorf("peer %s role = %s, want %s", node.name, peer.Role, role)
	}
	if peer.Name != node.name {
		t.Errorf("peer %s name = %q, want %q", node.name, peer.Name, node.name)
	}
	if peer.Platform != model.PlatformMacOS {
		t.Errorf("peer %s platform = %s, want macos", node.name, peer.Platform)
	}
	if peer.Revoked {
		t.Errorf("peer %s is revoked", node.name)
	}
}

// assertNoPeer checks that a device is not in the trust store.
func assertNoPeer(t *testing.T, trustStore *trust.Store, id model.DeviceID) {
	t.Helper()
	if _, err := trustStore.Get(context.Background(), id); !errors.Is(err, trust.ErrPeerNotFound) {
		t.Fatalf("device %s is in the trust store: %v", id, err)
	}
}

// assertGroupKeys checks that both devices share the same group secrets.
func assertGroupKeys(t *testing.T, alice, bob *testNode) {
	t.Helper()
	ctx := context.Background()

	discoveryA, err := alice.trust.DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("initiator discovery key: %v", err)
	}
	discoveryB, err := bob.trust.DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("responder discovery key: %v", err)
	}
	if !bytes.Equal(discoveryA, discoveryB) || len(discoveryA) != 32 {
		t.Fatalf("discovery keys differ: %x and %x", discoveryA, discoveryB)
	}

	rendezvousA, err := alice.trust.RendezvousKey(ctx, bob.deviceID())
	if err != nil {
		t.Fatalf("initiator rendezvous key: %v", err)
	}
	rendezvousB, err := bob.trust.RendezvousKey(ctx, alice.deviceID())
	if err != nil {
		t.Fatalf("responder rendezvous key: %v", err)
	}
	if !bytes.Equal(rendezvousA, rendezvousB) || len(rendezvousA) != 32 {
		t.Fatalf("rendezvous keys differ: %x and %x", rendezvousA, rendezvousB)
	}
}
