package pairing

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// TestWindowRefusesForeignIdentity checks that a device that dials the window
// cannot claim the identity of another one: the request is refused before a code
// is shown.
func TestWindowRefusesForeignIdentity(t *testing.T) {
	network, bus := testNetwork(t)
	node := newNode(t, network, bus, 1, 41000)
	node.open(t, ports.SystemClock, model.RolePersonal)

	client := testIdentity(t, 9)
	claimed := testIdentity(t, 7)
	dialer, closeDialer := newTestDialer(t, network, "127.0.0.1:43000")
	defer closeDialer()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, stream := requestPairing(ctx, t, dialer, node.window.Addr().String(), claimed)
	defer func() { _ = conn.Close() }()

	// The window ends the stream instead of answering with its identity.
	_ = stream.SetReadDeadline(time.Now().Add(testTimeout))
	if _, err := stream.Read(make([]byte, 1)); err == nil {
		t.Fatal("the window answered a request with the identity of another device")
	}
	assertNoPeer(t, node.trust, claimed.DeviceID())
	assertNoPeer(t, node.trust, client.DeviceID())
}

// TestDialRefusesForeignIdentity checks that a device that answers with the
// identity of another one is refused by the dialer.
func TestDialRefusesForeignIdentity(t *testing.T) {
	network, bus := testNetwork(t)
	node := newNode(t, network, bus, 1, 41000)
	node.open(t, ports.SystemClock, model.RolePersonal)

	responder := testIdentity(t, 9)
	claimed := testIdentity(t, 7)
	pc, err := network.ListenPacket("127.0.0.1:43200")
	if err != nil {
		t.Fatalf("packet connection: %v", err)
	}
	listener, err := transport.NewPairingListener(pc, responder, transport.AcceptPairingPeer, transport.Config{})
	if err != nil {
		_ = pc.Close()
		t.Fatalf("listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	go answerPairing(ctx, listener, claimed)

	err = node.window.Dial(ctx, listener.Addr().String())
	if !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("dial error = %v, want %v", err, ErrInvalidPeer)
	}
	assertNoPeer(t, node.trust, claimed.DeviceID())
	assertNoPeer(t, node.trust, responder.DeviceID())
}

// TestWindowLimitsPairingAttempts checks that a window stops accepting pairing
// connections after a burst: a device needs one connection for every try at the
// short code.
func TestWindowLimitsPairingAttempts(t *testing.T) {
	network, bus := testNetwork(t)
	node := newNode(t, network, bus, 1, 41000)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))
	node.open(t, clock, model.RolePersonal)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	sockets := newSockets(network, 44000)
	addr := node.window.Addr().String()
	attempt := func(seed byte) *quic.Stream {
		client := testIdentity(t, seed)
		pc, err := sockets.ListenPacket(ctx)
		if err != nil {
			t.Fatalf("client socket: %v", err)
		}
		dialer, err := transport.NewDialer(pc, client, unpairedVerifier{}, transport.Config{})
		if err != nil {
			_ = pc.Close()
			t.Fatalf("client dialer: %v", err)
		}
		t.Cleanup(func() { _ = dialer.Close() })

		conn, stream := requestPairing(ctx, t, dialer, addr, client)
		t.Cleanup(func() { _ = conn.Close() })
		return stream
	}

	for i := 0; i < maxAttempts; i++ {
		attempt(byte(10 + i))
		if event := waitEvent(t, node.window, EventRequest); event.Name != "client" {
			t.Fatalf("request of %q, want the client", event.Name)
		}
	}

	// One attempt more is refused: the window drops the connection instead of
	// showing another code.
	stream := attempt(30)
	_ = stream.SetReadDeadline(time.Now().Add(testTimeout))
	if _, err := stream.Read(make([]byte, 1)); err == nil {
		t.Fatal("the window accepted a pairing attempt over the limit")
	}

	// The limit is a rate: the window accepts again once the period is over.
	clock.Advance(attemptWindow)
	attempt(31)
	waitEvent(t, node.window, EventRequest)
}

// requestPairing opens a pairing connection with the identity of client and
// sends a SAS request. The connection belongs to the caller.
func requestPairing(ctx context.Context, t *testing.T, dialer *transport.Dialer, addr string, client *identity.Identity) (*transport.PairingConn, *quic.Stream) {
	t.Helper()
	conn, err := dialer.DialPairing(ctx, vnet.NewAddr(addr), identity.CertFingerprint{})
	if err != nil {
		t.Fatalf("dial pairing: %v", err)
	}
	stream, err := conn.OpenStream(ctx)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("open stream: %v", err)
	}
	public := client.Public()
	request := &wirev1.PairRequest{
		Ed25519Pub: public.Ed25519,
		MldsaPub:   public.MLDSABytes(),
		DeviceName: "client",
		Platform:   protoconv.PlatformToProto(model.PlatformMacOS),
		Role:       protoconv.RoleToProto(model.RolePersonal),
	}
	if err := writeMessage(stream, request); err != nil {
		_ = conn.Close()
		t.Fatalf("send request: %v", err)
	}
	return conn, stream
}

// answerPairing answers one pairing connection with the identity of claimed.
func answerPairing(ctx context.Context, listener *transport.PairingListener, claimed *identity.Identity) {
	conn, err := listener.Accept(ctx)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return
	}
	request := &wirev1.PairRequest{}
	if err := newFrameReader(stream).readMessage(request); err != nil {
		return
	}
	public := claimed.Public()
	if err := writeMessage(stream, &wirev1.PairResponse{
		Ed25519Pub: public.Ed25519,
		MldsaPub:   public.MLDSABytes(),
		DeviceName: "claimed",
		Platform:   protoconv.PlatformToProto(model.PlatformMacOS),
		Role:       request.Role,
	}); err != nil {
		return
	}
	// Hold the connection until the other device ends the stream, so the
	// response is not cut off by the close.
	_ = stream.SetReadDeadline(time.Now().Add(testTimeout))
	_, _ = io.Copy(io.Discard, stream)
}
