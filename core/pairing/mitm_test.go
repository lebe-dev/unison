package pairing

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// TestSASDiffersWithDeviceInTheMiddle checks that a device that terminates both
// pairing connections makes the two devices derive different SAS values. The
// identities it forwards are unchanged, so only the session keys differ.
func TestSASDiffersWithDeviceInTheMiddle(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	alice.open(t, ports.SystemClock, model.RolePersonal)
	bob.open(t, ports.SystemClock, model.RolePersonal)

	relay := newRelay(t, network, testIdentity(t, 3), bob.window.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	go relay.serve(ctx)

	pairing := dial(alice.window, relay.Addr())
	requestA := waitEvent(t, alice.window, EventRequest)
	requestB := waitEvent(t, bob.window, EventRequest)

	if got, want := requestA.DeviceID, bob.deviceID(); got != want {
		t.Errorf("initiator peer = %s, want %s", got, want)
	}
	if requestA.SAS.String() == requestB.SAS.String() {
		t.Fatalf("both devices derived the same SAS %q through a relay", requestA.SAS)
	}
	if requestA.SAS.IsZero() || requestB.SAS.IsZero() {
		t.Fatalf("empty SAS through a relay: %q and %q", requestA.SAS, requestB.SAS)
	}

	// Both devices refuse, so nothing is trusted.
	if err := alice.window.Reject(ctx, bob.deviceID()); err != nil {
		t.Fatalf("reject on the initiator: %v", err)
	}
	if err := bob.window.Reject(ctx, alice.deviceID()); err != nil {
		t.Fatalf("reject on the responder: %v", err)
	}
	if err := <-pairing; !errors.Is(err, ErrRejected) {
		t.Fatalf("pairing error = %v, want %v", err, ErrRejected)
	}
	assertNoPeer(t, alice.trust, bob.deviceID())
	assertNoPeer(t, bob.trust, alice.deviceID())
}

// relay terminates the pairing connection of both devices and forwards the
// protocol frames between them.
type relay struct {
	listener *transport.PairingListener
	dialer   *transport.Dialer
	target   string
}

// newRelay binds a pairing listener and a dialer on the virtual network.
func newRelay(t *testing.T, network *vnet.Network, id *identity.Identity, target string) *relay {
	t.Helper()

	pc, err := network.ListenPacket("127.0.0.1:43000")
	if err != nil {
		t.Fatalf("relay packet connection: %v", err)
	}
	listener, err := transport.NewPairingListener(pc, id, transport.AcceptPairingPeer, transport.Config{})
	if err != nil {
		_ = pc.Close()
		t.Fatalf("relay listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	sockets := newSockets(network, 43100)
	dialerPC, err := sockets.ListenPacket(context.Background())
	if err != nil {
		t.Fatalf("relay dialer socket: %v", err)
	}
	dialer, err := transport.NewDialer(dialerPC, id, unpairedVerifier{}, transport.Config{})
	if err != nil {
		_ = dialerPC.Close()
		t.Fatalf("relay dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	return &relay{listener: listener, dialer: dialer, target: target}
}

// Addr returns the address the relay listens on.
func (r *relay) Addr() string {
	return r.listener.Addr().String()
}

// serve accepts one pairing connection and relays its stream to the target.
func (r *relay) serve(ctx context.Context) {
	downstream, err := r.listener.Accept(ctx)
	if err != nil {
		return
	}
	defer func() { _ = downstream.Close() }()

	stream, err := downstream.AcceptStream(ctx)
	if err != nil {
		return
	}
	upstream, err := r.dialer.DialPairing(ctx, vnet.NewAddr(r.target), identity.CertFingerprint{})
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()

	peerStream, err := upstream.OpenStream(ctx)
	if err != nil {
		return
	}
	go func() {
		_, _ = io.Copy(peerStream, stream)
		_ = peerStream.Close()
	}()
	_, _ = io.Copy(stream, peerStream)
	_ = stream.Close()
}
