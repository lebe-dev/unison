package pairing

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// TestSASDiffersWithDeviceInTheMiddle checks that a device that terminates both
// pairing connections and presents its own identity makes the two devices
// derive different SAS values.
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

	// Both devices see the identity of the device in the middle, not the one of
	// the other device: the connection it presents is the only identity it can
	// back with a certificate.
	if got, want := requestA.DeviceID, relay.identity.DeviceID(); got != want {
		t.Errorf("initiator peer = %s, want %s", got, want)
	}
	if got, want := requestB.DeviceID, relay.identity.DeviceID(); got != want {
		t.Errorf("responder peer = %s, want %s", got, want)
	}
	if requestA.SAS.String() == requestB.SAS.String() {
		t.Fatalf("both devices derived the same SAS %q through a relay", requestA.SAS)
	}
	if requestA.SAS.IsZero() || requestB.SAS.IsZero() {
		t.Fatalf("empty SAS through a relay: %q and %q", requestA.SAS, requestB.SAS)
	}

	// Both devices refuse, so nothing is trusted.
	if err := alice.window.Reject(ctx, relay.identity.DeviceID()); err != nil {
		t.Fatalf("reject on the initiator: %v", err)
	}
	if err := bob.window.Reject(ctx, relay.identity.DeviceID()); err != nil {
		t.Fatalf("reject on the responder: %v", err)
	}
	if err := <-pairing; !errors.Is(err, ErrRejected) {
		t.Fatalf("pairing error = %v, want %v", err, ErrRejected)
	}
	assertNoPeer(t, alice.trust, bob.deviceID())
	assertNoPeer(t, bob.trust, alice.deviceID())
}

// TestWindowRefusesForwardedIdentity checks that a device that relays a pairing
// request without its own identity is refused: the identity of a message must
// belong to the certificate of the connection.
func TestWindowRefusesForwardedIdentity(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	alice.open(t, ports.SystemClock, model.RolePersonal)
	bob.open(t, ports.SystemClock, model.RolePersonal)

	relay := newRelay(t, network, testIdentity(t, 3), bob.window.Addr().String())
	relay.forward = true
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	go relay.serve(ctx)

	pairing := dial(alice.window, relay.Addr())

	// The window of the responder ends the exchange without showing a code.
	failure := waitEvent(t, bob.window, EventFailed)
	if !errors.Is(failure.Err, ErrInvalidPeer) {
		t.Errorf("failure = %v, want %v", failure.Err, ErrInvalidPeer)
	}
	if err := <-pairing; err == nil {
		t.Fatal("a forwarded identity paired")
	}
	assertNoPeer(t, alice.trust, bob.deviceID())
	assertNoPeer(t, bob.trust, alice.deviceID())
}

// relay terminates the pairing connection of both devices and keeps them apart.
type relay struct {
	listener *transport.PairingListener
	dialer   *transport.Dialer
	identity *identity.Identity
	target   string
	// forward relays the messages as they are instead of presenting the
	// identity of the relay.
	forward bool
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

	return &relay{listener: listener, dialer: dialer, identity: id, target: target}
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
		r.copyRequest(peerStream, stream)
		_, _ = io.Copy(peerStream, stream)
		_ = peerStream.Close()
	}()
	r.copyResponse(stream, peerStream)
	_, _ = io.Copy(stream, peerStream)
	_ = stream.Close()
}

// copyRequest relays the first frame of the initiator, with the identity of the
// relay unless it forwards the messages as they are, and then the rest of the
// stream.
func (r *relay) copyRequest(dst io.Writer, src io.Reader) {
	reader := newFrameReader(src)
	request := &wirev1.PairRequest{}
	if err := reader.readMessage(request); err != nil {
		return
	}
	if !r.forward {
		public := r.identity.Public()
		request.Ed25519Pub = public.Ed25519
		request.MldsaPub = public.MLDSABytes()
		request.DeviceName = "device-in-the-middle"
		request.Platform = protoconv.PlatformToProto(model.PlatformMacOS)
	}
	if err := writeMessage(dst, request); err != nil {
		return
	}
	_, _ = io.Copy(dst, reader.buf)
}

// copyResponse relays the first frame of the responder, with the identity of the
// relay unless it forwards the messages as they are, and then the rest of the
// stream.
func (r *relay) copyResponse(dst io.Writer, src io.Reader) {
	reader := newFrameReader(src)
	response := &wirev1.PairResponse{}
	if err := reader.readMessage(response); err != nil {
		return
	}
	if !r.forward {
		public := r.identity.Public()
		response.Ed25519Pub = public.Ed25519
		response.MldsaPub = public.MLDSABytes()
		response.DeviceName = "device-in-the-middle"
		response.Platform = protoconv.PlatformToProto(model.PlatformMacOS)
	}
	if err := writeMessage(dst, response); err != nil {
		return
	}
	_, _ = io.Copy(dst, reader.buf)
}
