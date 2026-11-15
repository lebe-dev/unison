package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// pairingPair is a pairing listener and a dialer on one virtual network.
type pairingPair struct {
	network  *vnet.Network
	listener *PairingListener
	dialer   *Dialer
	server   *identity.Identity
}

// newPairingPair binds a pairing listener and a dialer to the virtual network.
func newPairingPair(t *testing.T, server, client *identity.Identity, policy PairingPolicy) *pairingPair {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	listener, err := NewPairingListener(testPacketConn(t, network, "server:7777"), server, policy, Config{})
	if err != nil {
		t.Fatalf("pairing listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	dialer, err := NewDialer(testPacketConn(t, network, "client:7778"), client, newTestVerifier(), Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	return &pairingPair{network: network, listener: listener, dialer: dialer, server: server}
}

// pairDial dials the pairing listener and returns both sides.
func pairDial(t *testing.T, p *pairingPair, pin identity.CertFingerprint) (*PairingConn, *PairingConn) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	type result struct {
		conn *PairingConn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		conn, err := p.listener.Accept(ctx)
		accepted <- result{conn: conn, err: err}
	}()

	client, err := p.dialer.DialPairing(ctx, p.listener.Addr(), pin)
	if err != nil {
		t.Fatalf("dial pairing: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case r := <-accepted:
		if r.err != nil {
			t.Fatalf("accept pairing: %v", r.err)
		}
		t.Cleanup(func() { _ = r.conn.Close() })
		return client, r.conn
	case <-ctx.Done():
		t.Fatal("pairing accept did not return")
		return nil, nil
	}
}

// TestPairingPinnedCertificate checks the QR flow: both sides pin the
// certificate fingerprint they know out of band.
func TestPairingPinnedCertificate(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)

	pair := newPairingPair(t, server, client, PinCertificate(client.CertFingerprint()))
	clientConn, serverConn := pairDial(t, pair, server.CertFingerprint())

	if got, want := clientConn.Peer().DeviceID, server.DeviceID(); got != want {
		t.Fatalf("client peer = %s, want %s", got, want)
	}
	if got, want := clientConn.Peer().CertFingerprint, server.CertFingerprint(); got != want {
		t.Fatalf("client fingerprint = %s, want %s", got, want)
	}
	if got, want := serverConn.Peer().DeviceID, client.DeviceID(); got != want {
		t.Fatalf("server peer = %s, want %s", got, want)
	}
	if got, want := serverConn.Peer().CertFingerprint, client.CertFingerprint(); got != want {
		t.Fatalf("server fingerprint = %s, want %s", got, want)
	}
}

// TestPairingAcceptsUnknownDevice checks the SAS flow, which pins nothing and
// compares the devices out of band.
func TestPairingAcceptsUnknownDevice(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)

	pair := newPairingPair(t, server, client, AcceptPairingPeer)
	clientConn, serverConn := pairDial(t, pair, identity.CertFingerprint{})

	if got := clientConn.Peer().DeviceID; got != server.DeviceID() {
		t.Fatalf("client peer = %s, want %s", got, server.DeviceID())
	}
	if got := serverConn.Peer().DeviceID; got != client.DeviceID() {
		t.Fatalf("server peer = %s, want %s", got, client.DeviceID())
	}
}

// TestPairingRejectsUnpinnedDevice checks that a listener with a pinned
// certificate refuses another device at the TLS handshake.
func TestPairingRejectsUnpinnedDevice(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	other := testIdentity(t, 3)

	pair := newPairingPair(t, server, other, PinCertificate(client.CertFingerprint()))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	conn, err := pair.dialer.DialPairing(ctx, pair.listener.Addr(), server.CertFingerprint())
	if err == nil {
		t.Cleanup(func() { _ = conn.Close() })
		// The listener refuses after the client has completed its side of the
		// handshake, so the refusal arrives as a closed connection.
		waitClosed(conn.Context(), t)
	}

	select {
	case err := <-accepted:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("accept error = %v, want a refused connection", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accept did not return")
	}
}

// TestDialPairingWrongPin checks that the dialer refuses a server whose
// certificate does not match the pin.
func TestDialPairingWrongPin(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	other := testIdentity(t, 3)

	pair := newPairingPair(t, server, client, AcceptPairingPeer)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	if conn, err := pair.dialer.DialPairing(ctx, pair.listener.Addr(), other.CertFingerprint()); err == nil {
		_ = conn.Close()
		t.Fatal("a server with another certificate was accepted")
	}

	select {
	case err := <-accepted:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("accept error = %v, want a refused connection", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accept did not return")
	}
}

// TestPairingCarriesData checks that the pairing connection carries streams.
func TestPairingCarriesData(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)

	pair := newPairingPair(t, server, client, AcceptPairingPeer)
	clientConn, serverConn := pairDial(t, pair, identity.CertFingerprint{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if _, err := stream.Write([]byte("pair")); err != nil {
		t.Fatalf("write: %v", err)
	}

	peerStream, err := serverConn.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("accept stream: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(peerStream, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf, []byte("pair")) {
		t.Fatalf("payload = %q", buf)
	}
}

// waitClosed waits until the connection context ends.
func waitClosed(ctx context.Context, t *testing.T) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the refused connection stayed alive")
	}
}
