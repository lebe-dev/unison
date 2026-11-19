package relay

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports/fake"
	relayserver "github.com/lebe-dev/unison/internal/relay"
)

// testToken is the access token of the test relay.
const testToken = "test-token"

// testTimeout bounds one wait of the tests.
const testTimeout = 5 * time.Second

// testRelay is an in-process relay server.
type testRelay struct {
	// url is the address of the relay, which serves both transports on one
	// port.
	url    string
	tls    *tls.Config
	server *relayserver.Server
}

// startTestRelay runs a relay server that serves QUIC and WebSocket on the same
// port.
func startTestRelay(t *testing.T) *testRelay {
	t.Helper()
	return startTestRelayWith(t, nil)
}

// startTestRelayWith runs a relay server whose configuration tune changes.
func startTestRelayWith(t *testing.T, tune func(*relayserver.Config)) *testRelay {
	t.Helper()

	certFile, keyFile := testCertificate(t)
	port := freePort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)

	cfg := relayserver.DefaultConfig()
	cfg.QUICAddr = address
	cfg.WSAddr = address
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	cfg.Tokens = []string{testToken}
	cfg.MaxDatagramBytes = 1400
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if tune != nil {
		tune(&cfg)
	}

	server, err := relayserver.New(cfg)
	if err != nil {
		t.Fatalf("new relay: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close relay: %v", err)
		}
	})

	return &testRelay{
		url:    address,
		tls:    &tls.Config{InsecureSkipVerify: true},
		server: server,
	}
}

// client returns a configuration of a client of the relay.
func (r *testRelay) client(t *testing.T) Config {
	t.Helper()

	return Config{
		URL:            r.url,
		Token:          testToken,
		TLS:            r.tls,
		DialTimeout:    300 * time.Millisecond,
		ReconnectDelay: 50 * time.Millisecond,
		ReconnectLimit: 500 * time.Millisecond,
	}
}

// startClient runs a client that the test closes.
func startClient(t *testing.T, cfg Config) *Dialer {
	t.Helper()

	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = dialer.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = dialer.Close()
		wg.Wait()
	})
	return dialer
}

// openMailbox opens a packet connection of the pair key and the client.
func openMailbox(t *testing.T, dialer *Dialer, key []byte) *Conn {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := dialer.Open(ctx, key)
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// write sends one payload and fails the test when it does not fit.
func write(t *testing.T, conn *Conn, payload string) {
	t.Helper()

	if _, err := conn.WriteTo([]byte(payload), conn.RemoteAddr()); err != nil {
		t.Fatalf("write datagram: %v", err)
	}
}

// read waits for one payload.
func read(t *testing.T, conn *Conn) string {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	buf := make([]byte, 2048)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read datagram: %v", err)
	}
	return string(buf[:n])
}

// freePort returns a port that is free for TCP and UDP on the loopback address.
func freePort(t *testing.T) int {
	t.Helper()

	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen TCP: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()

		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		_ = pc.Close()
		return port
	}
	t.Fatalf("no free port for both transports")
	return 0
}

// testCertificate writes a self-signed certificate for the loopback address.
func testCertificate(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

func TestMailboxIDIsPerDayAndKey(t *testing.T) {
	key := []byte("rendezvous key of a pair")
	day := time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)
	next := day.Add(24 * time.Hour)

	first := MailboxID(key, day)
	if first != MailboxID(key, day.Add(time.Hour)) {
		t.Fatal("the id of one UTC day changed with the hour")
	}
	if first == MailboxID(key, next) {
		t.Fatal("the ids of two days are equal")
	}
	if first != MailboxID(key, day.In(time.FixedZone("ahead", 5*3600))) {
		t.Fatal("the id depends on the time zone of the caller")
	}
	if first == MailboxID([]byte("another key"), day) {
		t.Fatal("two keys share a mailbox")
	}
	if first.String() == "" || len(first.String()) != 2*mailboxIDLen {
		t.Fatalf("unexpected id form %q", first.String())
	}
}

func TestMailboxesAddsTheNextDayAtMidnight(t *testing.T) {
	key := []byte("rendezvous key of a pair")
	midnight := time.Date(2026, time.November, 17, 0, 0, 0, 0, time.UTC)

	early := Mailboxes(key, midnight.Add(-2*time.Hour), DefaultRollover)
	if len(early) != 1 {
		t.Fatalf("got %d mailboxes two hours before midnight", len(early))
	}

	late := Mailboxes(key, midnight.Add(-10*time.Minute), DefaultRollover)
	if len(late) != 2 {
		t.Fatalf("got %d mailboxes ten minutes before midnight", len(late))
	}
	if late[0] != MailboxID(key, midnight.Add(-time.Hour)) {
		t.Fatal("the first mailbox is not the current day")
	}
	if late[1] != MailboxID(key, midnight) {
		t.Fatal("the second mailbox is not the next day")
	}
}

func TestMailboxesKeepThePreviousDayAfterMidnight(t *testing.T) {
	key := []byte("rendezvous key of a pair")
	midnight := time.Date(2026, time.November, 17, 0, 0, 0, 0, time.UTC)

	// A peer whose clock is behind still sends to the mailbox of the previous
	// day for a while.
	early := Mailboxes(key, midnight.Add(10*time.Minute), DefaultRollover)
	if len(early) != 2 {
		t.Fatalf("got %d mailboxes ten minutes after midnight", len(early))
	}
	if early[0] != MailboxID(key, midnight) {
		t.Fatal("the first mailbox is not the current day")
	}
	if early[1] != MailboxID(key, midnight.Add(-time.Hour)) {
		t.Fatal("the second mailbox is not the previous day")
	}

	if late := Mailboxes(key, midnight.Add(2*time.Hour), DefaultRollover); len(late) != 1 {
		t.Fatalf("got %d mailboxes two hours after midnight", len(late))
	}
}

func TestPacketSize(t *testing.T) {
	tests := []struct {
		datagram int
		want     int
	}{
		{datagram: 64, want: 0},
		{datagram: 1024, want: 0},
		{datagram: 1224, want: 1200},
		{datagram: 1400, want: 1240 - datagramOverhead(1240)},
		{datagram: 4096, want: 1240 - datagramOverhead(1240)},
	}
	for _, test := range tests {
		if got := PacketSize(test.datagram); got != test.want {
			t.Errorf("PacketSize(%d) = %d, want %d", test.datagram, got, test.want)
		}
	}
	if got := PacketSize(4096); got+datagramOverhead(got) > maxFrameBytes {
		t.Fatalf("packet of %d bytes does not fit into the relay connection", got)
	}
}

func TestClientExchangeOverQUIC(t *testing.T) {
	server := startTestRelay(t)
	key := []byte("shared rendezvous key")

	first := startClient(t, server.client(t))
	second := startClient(t, server.client(t))
	firstConn := openMailbox(t, first, key)
	secondConn := openMailbox(t, second, key)

	if status := first.Status(); !status.Connected || status.Transport != TransportQUIC {
		t.Fatalf("unexpected status %+v", status)
	}
	if got := first.PacketSize(); got < MinPacketSize {
		t.Fatalf("packet size %d", got)
	}

	write(t, firstConn, "hello from the first device")
	if got := read(t, secondConn); got != "hello from the first device" {
		t.Fatalf("the second device received %q", got)
	}
	write(t, secondConn, "hello from the second device")
	if got := read(t, firstConn); got != "hello from the second device" {
		t.Fatalf("the first device received %q", got)
	}

	if local := firstConn.LocalAddr().Network(); local != "relay" {
		t.Fatalf("unexpected address network %q", local)
	}
}

func TestClientFallsBackToWebSocket(t *testing.T) {
	server := startTestRelay(t)
	key := []byte("shared rendezvous key")

	// A socket that swallows the datagrams simulates a network that blocks UDP.
	blackhole, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind blackhole: %v", err)
	}
	t.Cleanup(func() { _ = blackhole.Close() })

	cfg := server.client(t)
	cfg.QUICAddr = blackhole.LocalAddr().String()

	blocked := startClient(t, cfg)
	direct := startClient(t, server.client(t))
	blockedConn := openMailbox(t, blocked, key)
	directConn := openMailbox(t, direct, key)

	if status := blocked.Status(); !status.Connected || status.Transport != TransportWebSocket {
		t.Fatalf("the client did not fall back to WebSocket: %+v", status)
	}

	write(t, blockedConn, "over the fallback")
	if got := read(t, directConn); got != "over the fallback" {
		t.Fatalf("the direct device received %q", got)
	}
	write(t, directConn, "over QUIC")
	if got := read(t, blockedConn); got != "over QUIC" {
		t.Fatalf("the falling back device received %q", got)
	}
}

func TestClientRejectsSmallDatagrams(t *testing.T) {
	certFile, keyFile := testCertificate(t)
	port := freePort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)

	cfg := relayserver.DefaultConfig()
	cfg.QUICAddr = address
	cfg.WSAddr = address
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	cfg.Tokens = []string{testToken}
	cfg.MaxDatagramBytes = 1024
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	server, err := relayserver.New(cfg)
	if err != nil {
		t.Fatalf("new relay: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	client := startClient(t, Config{
		URL:            address,
		Token:          testToken,
		TLS:            &tls.Config{InsecureSkipVerify: true},
		DialTimeout:    300 * time.Millisecond,
		ReconnectDelay: 50 * time.Millisecond,
		ReconnectLimit: 500 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := client.Open(ctx, []byte("shared rendezvous key")); !errors.Is(err, ErrDatagramTooSmall) {
		t.Fatalf("open mailbox: %v", err)
	}
}

func TestClientRefusesBadToken(t *testing.T) {
	server := startTestRelay(t)

	cfg := server.client(t)
	cfg.Token = "wrong"
	client := startClient(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := client.Open(ctx, []byte("shared rendezvous key")); !errors.Is(err, ErrRejected) {
		t.Fatalf("open mailbox: %v", err)
	}
}

func TestClientRegistersLaterMailboxes(t *testing.T) {
	server := startTestRelay(t)

	first := startClient(t, server.client(t))
	second := startClient(t, server.client(t))

	firstKey := []byte("first pair")
	secondKey := []byte("second pair")

	firstConn := openMailbox(t, first, firstKey)
	secondConn := openMailbox(t, second, firstKey)
	write(t, firstConn, "first mailbox")
	if got := read(t, secondConn); got != "first mailbox" {
		t.Fatalf("the peer received %q", got)
	}

	// A second mailbox of the same client needs another registration.
	firstOther := openMailbox(t, first, secondKey)
	secondOther := openMailbox(t, second, secondKey)

	write(t, firstOther, "second mailbox")
	if got := read(t, secondOther); got != "second mailbox" {
		t.Fatalf("the peer received %q", got)
	}
	write(t, secondConn, "first mailbox again")
	if got := read(t, firstConn); got != "first mailbox again" {
		t.Fatalf("the first mailbox received %q", got)
	}
}

func TestClientRegistersTheNextDay(t *testing.T) {
	server := startTestRelay(t)
	clock := fake.NewClock(time.Date(2026, time.November, 16, 23, 50, 0, 0, time.UTC))

	cfg := server.client(t)
	cfg.Clock = clock
	first := startClient(t, cfg)
	second := startClient(t, cfg)

	key := []byte("shared rendezvous key")
	firstConn := openMailbox(t, first, key)
	secondConn := openMailbox(t, second, key)

	write(t, firstConn, "before midnight")
	if got := read(t, secondConn); got != "before midnight" {
		t.Fatalf("the peer received %q", got)
	}

	// The day changed: both clients register the mailbox of the new day and
	// keep working.
	clock.Advance(15 * time.Minute)

	deadline := time.Now().Add(testTimeout)
	for {
		if time.Now().After(deadline) {
			t.Fatal("the clients did not reconnect after midnight")
		}
		write(t, firstConn, "after midnight")
		if err := secondConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
		buf := make([]byte, 64)
		n, _, err := secondConn.ReadFrom(buf)
		if err == nil && string(buf[:n]) == "after midnight" {
			break
		}
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("read datagram: %v", err)
		}
	}
	if status := first.Status(); !status.Connected {
		t.Fatalf("the client is not connected after midnight: %+v", status)
	}
}

// TestClosedClientWakesWaiters closes a client and checks that a caller that
// waits for a state change learns about it, since a closed client never
// connects again.
func TestClosedClientWakesWaiters(t *testing.T) {
	dialer, err := NewDialer(Config{URL: "relay.example.com:443", Token: testToken})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if dialer.Closed() {
		t.Fatal("a new client reports that it is closed")
	}

	changes := dialer.Changes()
	if err := dialer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("closing the client did not signal a change")
	}
	if !dialer.Closed() {
		t.Fatal("a closed client does not report that it is closed")
	}
}
