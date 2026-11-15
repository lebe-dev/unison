//go:build sharelarge

package share

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// largeTimeout bounds the large transfer test.
const largeTimeout = 10 * time.Minute

// largeSize is the size of the transferred file.
const largeSize = 500 << 20

// harness is a virtual network with two devices that share files.
type harness struct {
	t     *testing.T
	net   *vnet.Network
	addrs int
	left  *device
	right *device
}

// device is one endpoint of the harness.
type device struct {
	id        *identity.Identity
	blobs     *blob.Store
	service   *Service
	downloads string
	trust     *testTrust
	sessions  *sync.Map
}

// newHarness starts two devices with a Personal pair.
func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{t: t, net: vnet.NewNetwork()}
	t.Cleanup(func() { _ = h.net.Close() })

	h.left = h.newDevice()
	h.right = h.newDevice()
	h.left.trust.add(h.right.id, model.RolePersonal)
	h.right.trust.add(h.left.id, model.RolePersonal)

	h.connect()
	return h
}

// newDevice opens the stores of one device.
func (h *harness) newDevice() *device {
	t := h.t
	dir := t.TempDir()

	migrations, err := blob.Migrations()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{
		Dir:        filepath.Join(dir, "data"),
		Keystore:   fake.NewKeystore(),
		Migrations: migrations,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	blobs, err := blob.New(blob.Options{Dir: filepath.Join(dir, "data", "blobs"), Store: db})
	if err != nil {
		t.Fatalf("open blob store: %v", err)
	}
	fetcher, err := transfer.NewFetcher(transfer.FetchOptions{Blobs: blobs, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("blob fetcher: %v", err)
	}

	downloads := filepath.Join(dir, "Downloads")
	dev := &device{
		id:        testIdentity(t),
		blobs:     blobs,
		downloads: downloads,
		trust:     newTestTrust(),
		sessions:  &sync.Map{},
	}
	service, err := New(Options{
		Blobs:     blobs,
		Trust:     dev.trust,
		Fetcher:   fetcher,
		Downloads: downloads,
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatalf("share service: %v", err)
	}
	dev.service = service
	t.Cleanup(service.Close)
	return dev
}

// connect links the two devices with a fresh connection and opens the
// sessions.
func (h *harness) connect() {
	t := h.t
	h.addrs += 2

	leftVer := &testVerifier{peers: map[string]transport.Peer{
		string(h.right.id.Public().Ed25519): {Public: h.right.id.Public(), Role: model.RolePersonal},
	}}
	rightVer := &testVerifier{peers: map[string]transport.Peer{
		string(h.left.id.Public().Ed25519): {Public: h.left.id.Public(), Role: model.RolePersonal},
	}}

	listener, err := transport.NewListener(h.packet(h.addrs), h.right.id, rightVer, transport.Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	dialer, err := transport.NewDialer(h.packet(h.addrs+1), h.left.id, leftVer, transport.Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type accepted struct {
		conn *transport.Conn
		err  error
	}
	acceptedCh := make(chan accepted, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		acceptedCh <- accepted{conn: conn, err: err}
	}()
	leftConn, err := dialer.Dial(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	result := <-acceptedCh
	if result.err != nil {
		t.Fatalf("accept: %v", result.err)
	}

	left, right := h.openSession(leftConn), h.openSession(result.conn)
	leftErr := make(chan error, 1)
	go func() { leftErr <- left.Open(ctx) }()
	if err := right.Open(ctx); err != nil {
		t.Fatalf("open the right session: %v", err)
	}
	if err := <-leftErr; err != nil {
		t.Fatalf("open the left session: %v", err)
	}

	h.left.sessions.Store("right", left)
	h.right.sessions.Store("left", right)
	t.Cleanup(func() {
		left.Close()
		right.Close()
		_ = listener.Close()
		_ = dialer.Close()
	})
}

// openSession prepares a session with the handlers of the device.
func (h *harness) openSession(conn *transport.Conn) *session.Session {
	t := h.t
	sess, err := session.New(conn, session.Config{
		Name:     "device",
		Platform: model.PlatformMacOS,
		Trust:    h.currentTrust(conn),
		Logger:   discardLogger(),
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := h.serviceOf(conn).Register(sess); err != nil {
		t.Fatalf("register the handlers: %v", err)
	}
	return sess
}

// serviceOf returns the device that owns the connection.
func (h *harness) serviceOf(conn *transport.Conn) *Service {
	if conn.Peer().DeviceID() == h.right.id.DeviceID() {
		return h.left.service
	}
	return h.right.service
}

// currentTrust returns the trust store of the device that owns the connection.
func (h *harness) currentTrust(conn *transport.Conn) *testTrust {
	if conn.Peer().DeviceID() == h.right.id.DeviceID() {
		return h.left.trust
	}
	return h.right.trust
}

// packet binds one virtual address.
func (h *harness) packet(n int) *vnet.Conn {
	h.t.Helper()
	conn, err := h.net.ListenPacket(fmt.Sprintf("n%d:%d", n, 4000+n))
	if err != nil {
		h.t.Fatalf("packet connection %d: %v", n, err)
	}
	return conn
}

// TestShareLargeResume checks a 500 MiB transfer with an interruption and a
// resume.
func TestShareLargeResume(t *testing.T) {
	h := newHarness(t)
	left, right := h.left, h.right

	path, data := largeFile(t)
	ctx, cancel := context.WithTimeout(context.Background(), largeTimeout)
	defer cancel()

	ids, err := left.service.SendPaths(ctx, []string{path}, []model.DeviceID{right.id.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	incoming := waitDownloading(t, right.service, left.id.DeviceID())
	waitBytes(t, right.service, incoming.ID)
	closeSession(t, right)
	if info, err := right.blobs.Stat(ctx, incoming.Files[0].Hash); err != nil || !info.Partial {
		t.Fatalf("the interrupted payload is complete: %v", err)
	}

	h.connect()

	done := waitState(t, right.service, incoming.ID, StateDone, largeTimeout)
	saved, err := os.ReadFile(filepath.Join(right.downloads, filepath.Base(done.Paths[0])))
	if err != nil {
		t.Fatalf("read the saved file: %v", err)
	}
	if !bytes.Equal(saved, data) {
		t.Fatalf("the resumed file differs from the source (%d bytes)", len(saved))
	}
	waitState(t, left.service, ids[0], StateDone, largeTimeout)
}

// largeFile writes a random file of largeSize and returns its path and content.
func largeFile(t *testing.T) (string, []byte) {
	t.Helper()

	data := make([]byte, largeSize)
	if _, err := rand.Read(data[:1<<20]); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	// A single random chunk is enough: the rest is a repeated pattern, so the
	// test does not have to keep 500 MiB of random data in two places.
	for copied := 1 << 20; copied < len(data); copied *= 2 {
		copy(data[copied:], data[:min(copied, len(data)-copied)])
	}
	path := filepath.Join(t.TempDir(), "archive.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	return path, data
}

// waitDownloading waits for the incoming transfer of the peer.
func waitDownloading(t *testing.T, service *Service, peer model.DeviceID) Transfer {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, record := range service.Transfers() {
			if record.Peer == peer && record.Direction == DirectionIncoming {
				return record
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no incoming transfer from %s", peer)
	return Transfer{}
}

// waitBytes waits until the transfer moved at least one chunk.
func waitBytes(t *testing.T, service *Service, id string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if record, err := service.Transfer(id); err == nil && record.Bytes > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("transfer %s made no progress", id)
}

// waitState waits for the transfer to reach the state.
func waitState(t *testing.T, service *Service, id string, state State, timeout time.Duration) Transfer {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		record, err := service.Transfer(id)
		if err == nil && record.State == state {
			return record
		}
		if err == nil && record.State.ended() {
			t.Fatalf("transfer %s is %s (%s), want %s", id, record.State, record.Err, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	record, err := service.Transfer(id)
	t.Fatalf("transfer %s is %s (%v), want %s within %v", id, record.State, err, state, timeout)
	return Transfer{}
}

// closeSession ends the open session of the device.
func closeSession(t *testing.T, dev *device) {
	t.Helper()

	value, ok := dev.sessions.Load("left")
	if !ok {
		value, ok = dev.sessions.Load("right")
	}
	if !ok {
		t.Fatal("the device has no session")
	}
	value.(*session.Session).Close()
}

// testTrust is the peer store of the harness.
type testTrust struct {
	mu    sync.Mutex
	peers map[model.DeviceID]trust.Peer
}

// newTestTrust returns an empty trust store.
func newTestTrust() *testTrust {
	return &testTrust{peers: make(map[model.DeviceID]trust.Peer)}
}

// Get returns the stored peer.
func (m *testTrust) Get(_ context.Context, id model.DeviceID) (trust.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	peer, ok := m.peers[id]
	if !ok {
		return trust.Peer{}, trust.ErrPeerNotFound
	}
	return peer, nil
}

// add stores a peer with its role.
func (m *testTrust) add(id *identity.Identity, role model.Role) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[id.DeviceID()] = trust.Peer{Public: id.Public(), Role: role}
}

// Rename stores the display name of a peer.
func (m *testTrust) Rename(_ context.Context, id model.DeviceID, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	peer, ok := m.peers[id]
	if !ok {
		return trust.ErrPeerNotFound
	}
	peer.Name = name
	m.peers[id] = peer
	return nil
}

// testVerifier is a transport.PeerVerifier over a fixed set of peers.
type testVerifier struct {
	peers map[string]transport.Peer
}

// VerifyPeer returns the peer that owns the certificate key.
func (v *testVerifier) VerifyPeer(_ context.Context, cert *x509.Certificate) (transport.Peer, error) {
	key, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return transport.Peer{}, errors.New("not an ed25519 certificate")
	}
	peer, ok := v.peers[string(key)]
	if !ok {
		return transport.Peer{}, errors.New("unknown device")
	}
	return peer, nil
}

// testIdentity returns a random identity.
func testIdentity(t *testing.T) *identity.Identity {
	t.Helper()

	edSeed := make([]byte, identity.SeedSize)
	pqSeed := make([]byte, identity.SeedSize)
	if _, err := rand.Read(edSeed); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	if _, err := rand.Read(pqSeed); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return id
}

// discardLogger returns a logger that drops everything, or the test logger when
// SHARE_TEST_LOG is set.
func discardLogger() *slog.Logger {
	if os.Getenv("SHARE_TEST_LOG") == "" {
		return slog.New(slog.DiscardHandler)
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
