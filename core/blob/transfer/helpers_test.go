package transfer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testNetwork is a virtual network with the nodes of a test.
type testNetwork struct {
	t     *testing.T
	net   *vnet.Network
	addrs int
}

// newTestNetwork returns an empty virtual network.
func newTestNetwork(t *testing.T) *testNetwork {
	t.Helper()
	h := &testNetwork{t: t, net: vnet.NewNetwork()}
	t.Cleanup(func() { _ = h.net.Close() })
	return h
}

// node is one device of a test.
type node struct {
	h     *testNetwork
	t     *testing.T
	id    *identity.Identity
	blobs *blob.Store
	trust *memTrust
	svc   *Service
	fetch *Fetcher

	mu       sync.Mutex
	sessions map[model.DeviceID]*session.Session
	// handler serves blob request streams instead of the service when set.
	handler func(peer model.DeviceID) session.RequestHandler
}

// newNode opens the stores of a device.
func (h *testNetwork) newNode(seed byte) *node {
	return h.newNodeWith(seed, nil)
}

// newNodeWith opens the stores of a device whose service authorizes shares
// with share.
func (h *testNetwork) newNodeWith(seed byte, share ShareAuthorizer) *node {
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
	svc, err := NewService(ServiceOptions{Blobs: blobs, Share: share, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("blob service: %v", err)
	}
	fetcher, err := NewFetcher(FetchOptions{
		Blobs:           blobs,
		RangeChunks:     1,
		PerPeerRequests: 1,
		Logger:          discardLogger(),
	})
	if err != nil {
		t.Fatalf("blob fetcher: %v", err)
	}

	return &node{
		h:        h,
		t:        t,
		id:       testIdentity(t, seed),
		blobs:    blobs,
		trust:    newMemTrust(),
		svc:      svc,
		fetch:    fetcher,
		sessions: make(map[model.DeviceID]*session.Session),
	}
}

// DeviceID returns the device ID of the node.
func (n *node) DeviceID() model.DeviceID {
	return n.id.DeviceID()
}

// connect links two nodes with both peers in the Personal role.
func (h *testNetwork) connect(a, b *node) {
	h.t.Helper()
	h.connectAs(a, b, model.RolePersonal, model.RolePersonal)
}

// connectAs links two nodes. roleOfA is the role a has for b, roleOfB the role
// b has for a.
func (h *testNetwork) connectAs(a, b *node, roleOfA, roleOfB model.Role) {
	t := h.t
	h.addrs += 2

	aVer := newTestVerifier().trust(transport.Peer{Public: b.id.Public(), Role: roleOfB})
	bVer := newTestVerifier().trust(transport.Peer{Public: a.id.Public(), Role: roleOfA})
	a.trust.add(b.id, roleOfB)
	b.trust.add(a.id, roleOfA)

	listener, err := transport.NewListener(h.packet(2*h.addrs), b.id, bVer, transport.Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	dialer, err := transport.NewDialer(h.packet(2*h.addrs+1), a.id, aVer, transport.Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	aConn, err := dialer.Dial(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial %s: %v", b.DeviceID(), err)
	}
	result := <-acceptedCh
	if result.err != nil {
		t.Fatalf("accept %s: %v", a.DeviceID(), result.err)
	}
	t.Cleanup(func() { _ = aConn.Close() })
	t.Cleanup(func() { _ = result.conn.Close() })

	aSess := a.newSession(t, aConn)
	bSess := b.newSession(t, result.conn)
	a.setSession(b.DeviceID(), aSess)
	b.setSession(a.DeviceID(), bSess)

	aErr := make(chan error, 1)
	go func() { aErr <- aSess.Open(ctx) }()
	if err := bSess.Open(ctx); err != nil {
		t.Fatalf("open %s: %v", b.DeviceID(), err)
	}
	if err := <-aErr; err != nil {
		t.Fatalf("open %s: %v", a.DeviceID(), err)
	}
	t.Cleanup(func() {
		aSess.Close()
		bSess.Close()
	})
}

// newSession prepares a session with the handlers of the node.
func (n *node) newSession(t *testing.T, conn *transport.Conn) *session.Session {
	t.Helper()
	sess, err := session.New(conn, session.Config{
		Name:     "node-" + n.DeviceID().String()[:8],
		Platform: model.PlatformMacOS,
		Trust:    n.trust,
	})
	if err != nil {
		t.Fatalf("session %s: %v", n.DeviceID(), err)
	}
	if err := n.register(sess); err != nil {
		t.Fatalf("register handlers of %s: %v", n.DeviceID(), err)
	}
	return sess
}

// register connects the handlers of the node to the session.
func (n *node) register(sess *session.Session) error {
	if err := n.svc.RegisterQueries(sess); err != nil {
		return err
	}
	if err := n.fetch.Register(sess); err != nil {
		return err
	}
	n.mu.Lock()
	handler := n.handler
	n.mu.Unlock()
	if handler == nil {
		return n.svc.RegisterRequests(sess)
	}
	return sess.HandleRequest(PayloadRequest, trust.ActionFetchBlob, handler(sess.DeviceID()))
}

// packet binds an address of the virtual network.
func (h *testNetwork) packet(n int) *vnet.Conn {
	h.t.Helper()
	conn, err := h.net.ListenPacket(fmt.Sprintf("n%d:%d", n, 4000+n))
	if err != nil {
		h.t.Fatalf("packet connection %d: %v", n, err)
	}
	return conn
}

// setSession stores the session with a peer.
func (n *node) setSession(peer model.DeviceID, sess *session.Session) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sessions[peer] = sess
}

// sessionTo returns the session with another node as a transfer peer.
func (n *node) sessionTo(other *node) Peer {
	n.mu.Lock()
	defer n.mu.Unlock()
	sess, ok := n.sessions[other.DeviceID()]
	if !ok {
		n.t.Fatalf("%s has no session with %s", n.DeviceID(), other.DeviceID())
	}
	return sess
}

// closeSession ends the session with a peer.
func (n *node) closeSession(peer model.DeviceID) {
	n.mu.Lock()
	sess := n.sessions[peer]
	n.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// setHandler replaces the blob request handler of the node.
func (n *node) setHandler(handler func(peer model.DeviceID) session.RequestHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handler = handler
}

// memTrust is an in-memory session.Trust.
type memTrust struct {
	mu    sync.Mutex
	peers map[model.DeviceID]trust.Peer
}

// newMemTrust returns an empty trust store.
func newMemTrust() *memTrust {
	return &memTrust{peers: make(map[model.DeviceID]trust.Peer)}
}

// Get returns the stored peer.
func (m *memTrust) Get(_ context.Context, id model.DeviceID) (trust.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	peer, ok := m.peers[id]
	if !ok {
		return trust.Peer{}, trust.ErrPeerNotFound
	}
	return peer, nil
}

// Rename stores the display name of a peer.
func (m *memTrust) Rename(_ context.Context, id model.DeviceID, name string) error {
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

// add stores a peer with the role it has for the local device.
func (m *memTrust) add(id *identity.Identity, role model.Role) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[id.DeviceID()] = trust.Peer{Public: id.Public(), Role: role}
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

// discardLogger returns a logger that drops everything.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// put stores content and returns its metadata.
func put(t *testing.T, s *blob.Store, data []byte) blob.Info {
	t.Helper()
	info, err := s.Put(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("put %d bytes: %v", len(data), err)
	}
	return info
}

// content reads a stored blob back.
func content(t *testing.T, s *blob.Store, hash [32]byte) []byte {
	t.Helper()
	info, err := s.Stat(context.Background(), hash)
	if err != nil {
		t.Fatalf("stat %s: %v", hashHex(hash), err)
	}
	reader, err := s.Open(context.Background(), hash)
	if err != nil {
		t.Fatalf("open %s: %v", hashHex(hash), err)
	}
	defer func() { _ = reader.Close() }()

	data := make([]byte, info.Size)
	if _, err := reader.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read %s: %v", hashHex(hash), err)
	}
	return data
}

// corruptHandler serves a blob like the service but flips one byte of every
// chunk from chunk from. The chunk hash matches the tampered data, so only the
// hash tree tells the chunk is wrong.
func corruptHandler(t *testing.T, blobs *blob.Store, hash [32]byte, from int) session.RequestHandler {
	t.Helper()
	return func(ctx context.Context, stream *session.Request) error {
		msg := &wirev1.BlobRequest{}
		if err := stream.Read(ctx, msg); err != nil {
			return err
		}
		info, err := blobs.Stat(ctx, hash)
		if err != nil {
			return err
		}
		tree, err := blobs.Tree(ctx, hash)
		if err != nil {
			return err
		}
		if err := stream.Write(ctx, treeReply(info, tree)); err != nil {
			return err
		}

		reader, err := blobs.Open(ctx, hash)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()

		for i := int(msg.GetFromChunk()); i <= int(msg.GetToChunk()); i++ {
			data, err := reader.ReadChunk(i)
			if err != nil {
				return err
			}
			if i >= from {
				data[0] ^= 0xff
			}
			sum := chunkHash(data)
			if err := stream.Write(ctx, chunkReply(i, data, sum[:])); err != nil {
				return err
			}
		}
		return nil
	}
}
