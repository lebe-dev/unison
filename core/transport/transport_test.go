package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// testIdentity returns an identity derived from the seed.
func testIdentity(t testing.TB, seed byte) *identity.Identity {
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

// testVerifier is a PeerVerifier over a fixed set of peers.
type testVerifier struct {
	mu    sync.Mutex
	peers map[string]Peer
}

// newTestVerifier returns a verifier that trusts nobody.
func newTestVerifier() *testVerifier {
	return &testVerifier{peers: make(map[string]Peer)}
}

// trust adds a peer to the verifier.
func (v *testVerifier) trust(peer Peer) *testVerifier {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.peers[string(peer.Public.Ed25519)] = peer
	return v
}

// distrust removes a peer from the verifier.
func (v *testVerifier) distrust(peer Peer) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.peers, string(peer.Public.Ed25519))
}

// VerifyPeer returns the peer that owns the certificate key.
func (v *testVerifier) VerifyPeer(_ context.Context, cert *x509.Certificate) (Peer, error) {
	key, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return Peer{}, errors.New("not an ed25519 certificate")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	peer, ok := v.peers[string(key)]
	if !ok {
		return Peer{}, errors.New("unknown device")
	}
	return peer, nil
}

// peerOf returns the transport peer of an identity with the role.
func peerOf(id *identity.Identity, role model.Role) Peer {
	return Peer{Public: id.Public(), Role: role}
}

// testPair is a listener and a dialer on one virtual network.
type testPair struct {
	network  *vnet.Network
	listener *Listener
	dialer   *Dialer
}

// newTestPair binds a listener and a dialer to the virtual network.
func newTestPair(t *testing.T, server, client *identity.Identity, serverVer, clientVer PeerVerifier) *testPair {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	listener, err := NewListener(testPacketConn(t, network, "server:4433"), server, serverVer, Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	dialer, err := NewDialer(testPacketConn(t, network, "client:4434"), client, clientVer, Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	return &testPair{network: network, listener: listener, dialer: dialer}
}

// testPacketConn binds a virtual packet connection.
func testPacketConn(t *testing.T, network *vnet.Network, addr string) *vnet.Conn {
	t.Helper()
	pc, err := network.ListenPacket(addr)
	if err != nil {
		t.Fatalf("packet connection %s: %v", addr, err)
	}
	return pc
}

// connect dials the listener and returns both sides of the new connection.
func connect(t *testing.T, p *testPair) (*Conn, *Conn) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	type result struct {
		conn *Conn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		conn, err := p.listener.Accept(ctx)
		accepted <- result{conn: conn, err: err}
	}()

	client, err := p.dialer.Dial(ctx, p.listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case r := <-accepted:
		if r.err != nil {
			t.Fatalf("accept: %v", r.err)
		}
		t.Cleanup(func() { _ = r.conn.Close() })
		return client, r.conn
	case <-ctx.Done():
		t.Fatal("accept did not return the connection")
		return nil, nil
	}
}

// TestTrustedPeersConnect checks that two trusted devices establish an
// authenticated connection.
func TestTrustedPeersConnect(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)

	serverVer := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	clientVer := newTestVerifier().trust(peerOf(server, model.RolePersonal))
	pair := newTestPair(t, server, client, serverVer, clientVer)

	clientConn, serverConn := connect(t, pair)

	if got, want := clientConn.Peer().DeviceID(), server.DeviceID(); got != want {
		t.Fatalf("client peer = %s, want %s", got, want)
	}
	if got, want := serverConn.Peer().DeviceID(), client.DeviceID(); got != want {
		t.Fatalf("server peer = %s, want %s", got, want)
	}
	if clientConn.Role() != model.RolePersonal || serverConn.Role() != model.RolePersonal {
		t.Fatalf("roles = %s and %s, want personal", clientConn.Role(), serverConn.Role())
	}

	state := clientConn.ConnectionState()
	if state.TLS.Version != tls.VersionTLS13 {
		t.Fatalf("TLS version = %#04x, want TLS 1.3", state.TLS.Version)
	}
	if state.TLS.CurveID != tls.X25519MLKEM768 {
		t.Fatalf("key exchange = %s, want X25519MLKEM768", state.TLS.CurveID)
	}
	if state.TLS.NegotiatedProtocol != ALPN {
		t.Fatalf("protocol = %q, want %q", state.TLS.NegotiatedProtocol, ALPN)
	}

	if got := clientConn.ControlStream(); got == nil {
		t.Fatal("client control stream is nil")
	}
	if got := serverConn.ControlStream(); got == nil {
		t.Fatal("server control stream is nil")
	}
}

// TestControlStreamCarriesData checks that the stream used for the
// authentication proof stays usable for the peer protocol.
func TestControlStreamCarriesData(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(client, model.RoleFriends))
	clientVer := newTestVerifier().trust(peerOf(server, model.RoleFriends))
	pair := newTestPair(t, server, client, serverVer, clientVer)

	clientConn, serverConn := connect(t, pair)

	writeStream(t, clientConn.ControlStream(), []byte("hello from the client"))
	if got := readStream(t, serverConn.ControlStream(), 21); !bytes.Equal(got, []byte("hello from the client")) {
		t.Fatalf("payload = %q", got)
	}

	writeStream(t, serverConn.ControlStream(), []byte("and back"))
	if got := readStream(t, clientConn.ControlStream(), 8); !bytes.Equal(got, []byte("and back")) {
		t.Fatalf("payload = %q", got)
	}
}

// TestUntrustedPeerRefusedByServer checks that a device the server does not
// trust never reaches the application.
func TestUntrustedPeerRefusedByServer(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)

	serverVer := newTestVerifier() // trusts nobody
	clientVer := newTestVerifier().trust(peerOf(server, model.RolePersonal))
	pair := newTestPair(t, server, client, serverVer, clientVer)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refused, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	accepted := make(chan error, 1)
	go func() {
		conn, err := pair.listener.Accept(refused)
		if err == nil {
			_ = conn.Close()
		}
		accepted <- err
	}()

	if conn, err := pair.dialer.Dial(ctx, pair.listener.Addr()); err == nil {
		_ = conn.Close()
		t.Fatal("dial of an untrusted peer succeeded")
	}

	select {
	case err := <-accepted:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("accept error = %v, want a refused connection and no error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accept did not return")
	}
}

// TestUntrustedPeerRefusedByClient checks that a dialer refuses a server its
// verifier does not know.
func TestUntrustedPeerRefusedByClient(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)

	serverVer := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	clientVer := newTestVerifier() // trusts nobody
	pair := newTestPair(t, server, client, serverVer, clientVer)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refused, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	accepted := make(chan error, 1)
	go func() {
		conn, err := pair.listener.Accept(refused)
		if err == nil {
			_ = conn.Close()
		}
		accepted <- err
	}()

	if conn, err := pair.dialer.Dial(ctx, pair.listener.Addr()); err == nil {
		_ = conn.Close()
		t.Fatal("dial of an unknown server succeeded")
	}

	select {
	case err := <-accepted:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("accept error = %v, want a refused connection and no error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accept did not return")
	}
}

// TestRevokedPeerRefused checks that a peer that disappears from the verifier
// mid-handshake is refused.
func TestRevokedPeerRefused(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)

	serverVer := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	clientVer := newTestVerifier().trust(peerOf(server, model.RolePersonal))
	pair := newTestPair(t, server, client, serverVer, clientVer)

	serverVer.distrust(peerOf(client, model.RolePersonal))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if conn, err := pair.dialer.Dial(ctx, pair.listener.Addr()); err == nil {
		_ = conn.Close()
		t.Fatal("dial of a revoked peer succeeded")
	}
}

// TestPacketLoss checks that a trusted connection works over a lossy link.
func TestPacketLoss(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	clientVer := newTestVerifier().trust(peerOf(server, model.RolePersonal))
	pair := newTestPair(t, server, client, serverVer, clientVer)

	if err := pair.network.SetLoss(0.05); err != nil {
		t.Fatalf("loss: %v", err)
	}
	if err := pair.network.SetLatency(2 * time.Millisecond); err != nil {
		t.Fatalf("latency: %v", err)
	}
	pair.network.SetSeed(7)

	clientConn, serverConn := connect(t, pair)

	const size = 256 << 10
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i)
	}

	stream, err := clientConn.OpenStream(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	read := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		peerStream, err := serverConn.AcceptStream(context.Background())
		if err != nil {
			readErr <- err
			return
		}
		if err := peerStream.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			readErr <- err
			return
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(peerStream, buf); err != nil {
			readErr <- err
			return
		}
		read <- buf
	}()

	if _, err := stream.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case err := <-readErr:
		t.Fatalf("read: %v", err)
	case got := <-read:
		if !bytes.Equal(got, payload) {
			t.Fatal("payload was corrupted by the lossy link")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("payload did not arrive")
	}

	if _, dropped := pair.network.Stats(); dropped == 0 {
		t.Fatal("the lossy link dropped nothing, the test proves nothing")
	}
}

// TestDefaults checks the connection settings.
func TestDefaults(t *testing.T) {
	cfg := Config{}.withDefaults()
	if cfg.KeepAlive != KeepAlivePeriod {
		t.Fatalf("keepalive = %v, want %v", cfg.KeepAlive, KeepAlivePeriod)
	}
	quicCfg := cfg.quic()
	if quicCfg.KeepAlivePeriod != 15*time.Second {
		t.Fatalf("keepalive around = %v, want 15s", quicCfg.KeepAlivePeriod)
	}
	if quicCfg.MaxIdleTimeout <= quicCfg.KeepAlivePeriod {
		t.Fatalf("idle timeout %v does not exceed the keepalive %v", quicCfg.MaxIdleTimeout, quicCfg.KeepAlivePeriod)
	}
	if quicCfg.MaxIncomingStreams <= 0 || quicCfg.MaxIncomingUniStreams <= 0 {
		t.Fatalf("stream limits = %d and %d, want positive values", quicCfg.MaxIncomingStreams, quicCfg.MaxIncomingUniStreams)
	}
}

// TestConstructorsRefuseIncompleteChecks checks that a listener and a dialer
// without a peer verifier, and a pairing listener without a policy, do not
// exist.
func TestConstructorsRefuseIncompleteChecks(t *testing.T) {
	id := testIdentity(t, 1)
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	if _, err := NewListener(nil, id, newTestVerifier(), Config{}); err == nil {
		t.Fatal("listener without a packet connection was created")
	}
	if _, err := NewListener(testPacketConn(t, network, "a:1"), id, nil, Config{}); err == nil {
		t.Fatal("listener without a verifier was created")
	}
	if _, err := NewDialer(testPacketConn(t, network, "b:1"), id, nil, Config{}); err == nil {
		t.Fatal("dialer without a verifier was created")
	}
	if _, err := NewPairingListener(testPacketConn(t, network, "c:1"), id, nil, Config{}); err == nil {
		t.Fatal("pairing listener without a policy was created")
	}
}

// TestRealUDP checks the connection over a UDP socket instead of vnet.
func TestRealUDP(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	clientVer := newTestVerifier().trust(peerOf(server, model.RolePersonal))

	serverPC := listenUDP(t)
	listener, err := NewListener(serverPC, server, serverVer, Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	dialer, err := NewDialer(listenUDP(t), client, clientVer, Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type result struct {
		conn *Conn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		accepted <- result{conn: conn, err: err}
	}()

	clientConn, err := dialer.Dial(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	select {
	case r := <-accepted:
		if r.err != nil {
			t.Fatalf("accept: %v", r.err)
		}
		t.Cleanup(func() { _ = r.conn.Close() })
		if got := r.conn.Peer().DeviceID(); got != client.DeviceID() {
			t.Fatalf("peer = %s, want %s", got, client.DeviceID())
		}
		writeStream(t, clientConn.ControlStream(), []byte("udp"))
		if got := readStream(t, r.conn.ControlStream(), 3); !bytes.Equal(got, []byte("udp")) {
			t.Fatalf("payload = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("accept did not return the connection")
	}
}

// listenUDP binds a UDP socket on the loopback interface.
func listenUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		t.Fatalf("packet connection is %T, want *net.UDPConn", pc)
	}
	return conn
}

// writeStream writes the payload to the stream.
func writeStream(t *testing.T, stream *quic.Stream, payload []byte) {
	t.Helper()
	if err := stream.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("write deadline: %v", err)
	}
	if _, err := stream.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readStream reads exactly size bytes from the stream.
func readStream(t *testing.T, stream *quic.Stream, size int) []byte {
	t.Helper()
	if err := stream.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	return buf
}

// rawClient opens a QUIC connection with the transport TLS settings but without
// the authentication proof exchange, so a test can send its own proof.
func rawClient(t *testing.T, pc net.PacketConn, addr net.Addr, id *identity.Identity, verify func(*x509.Certificate) error) *quic.Conn {
	t.Helper()

	tlsConfig := clientTLSConfig(id, ALPN, func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		cert, err := leafCertificate(rawCerts)
		if err != nil {
			return err
		}
		return verify(cert)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := quic.Dial(ctx, pc, addr, tlsConfig, Config{}.withDefaults().quic())
	if err != nil {
		t.Fatalf("raw dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseWithError(0, "") })
	return conn
}

// trustKey returns a certificate callback that accepts one Ed25519 key.
func trustKey(id *identity.Identity) func(*x509.Certificate) error {
	return func(cert *x509.Certificate) error {
		key, ok := cert.PublicKey.(ed25519.PublicKey)
		if !ok {
			return errors.New("not an ed25519 certificate")
		}
		if !bytes.Equal(key, id.Public().Ed25519) {
			return fmt.Errorf("unexpected certificate key")
		}
		return nil
	}
}
