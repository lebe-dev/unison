package session

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testIdentity returns an identity derived from the seed.
func testIdentity(t *testing.T, seed byte) *identity.Identity {
	t.Helper()
	edSeed := make([]byte, identity.SeedSize)
	pqSeed := make([]byte, identity.SeedSize)
	for i := range edSeed {
		edSeed[i] = seed + byte(i)
		pqSeed[i] = seed*7 + byte(i)
	}
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return id
}

// testVerifier is a transport.PeerVerifier over a fixed set of peers.
type testVerifier struct {
	mu    sync.Mutex
	peers map[string]transport.Peer
}

// newTestVerifier returns a verifier that trusts nobody.
func newTestVerifier() *testVerifier {
	return &testVerifier{peers: make(map[string]transport.Peer)}
}

// trust adds a peer to the verifier.
func (v *testVerifier) trust(peer transport.Peer) *testVerifier {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.peers[string(peer.Public.Ed25519)] = peer
	return v
}

// VerifyPeer returns the peer that owns the certificate key.
func (v *testVerifier) VerifyPeer(_ context.Context, cert *x509.Certificate) (transport.Peer, error) {
	key, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return transport.Peer{}, errors.New("not an ed25519 certificate")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	peer, ok := v.peers[string(key)]
	if !ok {
		return transport.Peer{}, errors.New("unknown device")
	}
	return peer, nil
}

// testTrust is an in-memory Trust.
type testTrust struct {
	mu    sync.Mutex
	peers map[model.DeviceID]trust.Peer
}

// newTestTrust creates a store with the given peers.
func newTestTrust(peers ...trust.Peer) *testTrust {
	store := &testTrust{peers: make(map[model.DeviceID]trust.Peer, len(peers))}
	for _, peer := range peers {
		store.peers[peer.DeviceID()] = peer
	}
	return store
}

var _ Trust = (*testTrust)(nil)

// Get returns the stored peer.
func (s *testTrust) Get(_ context.Context, id model.DeviceID) (trust.Peer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[id]
	if !ok {
		return trust.Peer{}, trust.ErrPeerNotFound
	}
	return peer, nil
}

// Rename stores the display name of the peer.
func (s *testTrust) Rename(_ context.Context, id model.DeviceID, name string) error {
	return s.update(id, func(peer *trust.Peer) { peer.Name = name })
}

// SetPlatform stores the platform of the peer.
func (s *testTrust) SetPlatform(_ context.Context, id model.DeviceID, platform model.Platform) error {
	return s.update(id, func(peer *trust.Peer) { peer.Platform = platform })
}

// update changes a stored peer.
func (s *testTrust) update(id model.DeviceID, change func(*trust.Peer)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[id]
	if !ok {
		return trust.ErrPeerNotFound
	}
	change(&peer)
	s.peers[id] = peer
	return nil
}

// peer returns the stored peer.
func (s *testTrust) peer(t *testing.T, id model.DeviceID) trust.Peer {
	t.Helper()
	peer, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("peer %s: %v", id, err)
	}
	return peer
}

// harness is a client and a server with an authenticated connection and no
// session yet.
type harness struct {
	t *testing.T

	clientID   *identity.Identity
	serverID   *identity.Identity
	clientConn *transport.Conn
	serverConn *transport.Conn

	clientTrust *testTrust
	serverTrust *testTrust
	clientCfg   Config
	serverCfg   Config
}

// newHarness connects two test devices. The client has clientRole for the
// server, the server has serverRole for the client.
func newHarness(t *testing.T, clientRole, serverRole model.Role) *harness {
	t.Helper()

	serverID := testIdentity(t, 1)
	clientID := testIdentity(t, 2)

	serverVer := newTestVerifier().trust(transport.Peer{Public: clientID.Public(), Role: serverRole})
	clientVer := newTestVerifier().trust(transport.Peer{Public: serverID.Public(), Role: clientRole})

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	listener, err := transport.NewListener(packetConn(t, network, "server:4433"), serverID, serverVer, transport.Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	dialer, err := transport.NewDialer(packetConn(t, network, "client:4434"), clientID, clientVer, transport.Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	type acceptResult struct {
		conn *transport.Conn
		err  error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		accepted <- acceptResult{conn: conn, err: err}
	}()

	clientConn, err := dialer.Dial(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	var serverConn *transport.Conn
	select {
	case result := <-accepted:
		if result.err != nil {
			t.Fatalf("accept: %v", result.err)
		}
		serverConn = result.conn
	case <-ctx.Done():
		t.Fatal("accept did not return")
	}
	t.Cleanup(func() { _ = serverConn.Close() })

	clientTrust := newTestTrust(trust.Peer{Public: serverID.Public(), Name: "server", Role: clientRole})
	serverTrust := newTestTrust(trust.Peer{Public: clientID.Public(), Name: "client", Role: serverRole})

	return &harness{
		t:           t,
		clientID:    clientID,
		serverID:    serverID,
		clientConn:  clientConn,
		serverConn:  serverConn,
		clientTrust: clientTrust,
		serverTrust: serverTrust,
		clientCfg:   Config{Name: "client", Platform: model.PlatformMacOS, Trust: clientTrust},
		serverCfg:   Config{Name: "server", Platform: model.PlatformWindows, Trust: serverTrust},
	}
}

// open creates both sessions, runs before on them and exchanges Hello.
func (h *harness) open(before func(client, server *Session)) (*Session, *Session) {
	h.t.Helper()

	client, err := New(h.clientConn, h.clientCfg)
	if err != nil {
		h.t.Fatalf("client session: %v", err)
	}
	server, err := New(h.serverConn, h.serverCfg)
	if err != nil {
		h.t.Fatalf("server session: %v", err)
	}
	if before != nil {
		before(client, server)
	}

	clientErr := make(chan error, 1)
	go func() { clientErr <- client.Open(context.Background()) }()
	if err := server.Open(context.Background()); err != nil {
		h.t.Fatalf("server open: %v", err)
	}
	if err := <-clientErr; err != nil {
		h.t.Fatalf("client open: %v", err)
	}
	h.t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return client, server
}

// rawOpen starts the server session and answers its Hello without a session on
// the client side.
func (h *harness) rawOpen(hello *wirev1.Hello) (*Session, <-chan error) {
	h.t.Helper()

	server, err := New(h.serverConn, h.serverCfg)
	if err != nil {
		h.t.Fatalf("server session: %v", err)
	}
	h.t.Cleanup(server.Close)

	closed := make(chan error, 1)
	go func() { closed <- server.Open(context.Background()) }()

	rawHello(h.t, h.clientConn, hello)
	return server, closed
}

// rawHello reads the Hello of the peer and answers it.
func rawHello(t *testing.T, conn *transport.Conn, hello *wirev1.Hello) {
	t.Helper()
	if err := conn.ControlStream().SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	reader := newFrameReader(conn.ControlStream())
	msg := &wirev1.ControlMessage{}
	if err := reader.readMessage(MaxControlMessage, msg); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if msg.GetHello() == nil {
		t.Fatalf("first message is %T, want hello", msg.GetBody())
	}
	writeControl(t, conn.ControlStream(), &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_Hello{Hello: hello},
	})
}

// writeControl writes one control message to the stream.
func writeControl(t *testing.T, w io.Writer, msg *wirev1.ControlMessage) {
	t.Helper()
	if err := writeFrame(w, msg, MaxControlMessage); err != nil {
		t.Fatalf("write control message: %v", err)
	}
}

// packetConn binds a non blocking packet connection on the virtual network.
func packetConn(t *testing.T, network *vnet.Network, addr string) *vnet.Conn {
	t.Helper()
	conn, err := network.ListenPacket(addr)
	if err != nil {
		t.Fatalf("packet connection %s: %v", addr, err)
	}
	return conn
}

// waitEvent returns the next event of the kind.
func waitEvent(t *testing.T, s *Session, kind EventKind) Event {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-s.Events():
			if !ok {
				t.Fatalf("events of %s closed while waiting for %s", s.DeviceID(), kind)
			}
			if event.Kind == kind {
				return event
			}
		case <-timer.C:
			t.Fatalf("event %s did not arrive", kind)
		}
	}
}

// waitDone waits until the session ends.
func waitDone(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("session with %s did not end", s.DeviceID())
	}
}

// waitCloseCode waits until the peer closes the connection and returns the
// application error code it sent.
func waitCloseCode(t *testing.T, stream *quic.Stream) quic.ApplicationErrorCode {
	t.Helper()
	if err := stream.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	buf := make([]byte, 64)
	for range 4 {
		_, err := stream.Read(buf)
		if err == nil {
			continue
		}
		var appErr *quic.ApplicationError
		if errors.As(err, &appErr) {
			return appErr.ErrorCode
		}
		if errors.Is(err, io.EOF) {
			continue
		}
		t.Fatalf("read until close: %v", err)
	}
	t.Fatal("the connection did not close")
	return 0
}
