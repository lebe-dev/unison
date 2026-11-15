package node

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// testTimeout bounds the wait for an event or a state in the tests.
const testTimeout = 20 * time.Second

// testLogger returns a logger that writes to stderr when the test asks for it.
func testLogger(base int) *slog.Logger {
	if os.Getenv("NODE_TEST_LOG") == "" {
		return nil
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return log.With("node", base)
}

// socketFactory binds virtual packet connections on consecutive addresses of
// one node. The first socket is the main listener.
type socketFactory struct {
	network *vnet.Network
	next    int

	mu    sync.Mutex
	addrs []string
}

// newSocketFactory returns a factory that starts at base.
func newSocketFactory(network *vnet.Network, base int) *socketFactory {
	return &socketFactory{network: network, next: base}
}

// listenPacket binds the next virtual address.
func (f *socketFactory) listenPacket(_ context.Context, _ int) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(f.next))
	f.next++
	pc, err := f.network.ListenPacket(addr)
	if err != nil {
		return nil, err
	}
	f.addrs = append(f.addrs, addr)
	return pc, nil
}

// bound returns the address bound by the call with the given index.
func (f *socketFactory) bound(index int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if index < 0 || index >= len(f.addrs) {
		return ""
	}
	return f.addrs[index]
}

// boundAddrs returns every bound address.
func (f *socketFactory) boundAddrs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.addrs)
}

// testNode is one device of a node test.
type testNode struct {
	*Node
	clipboard *fake.Clipboard
	sockets   *socketFactory
}

// newTestNode creates a node on the virtual network.
func newTestNode(t *testing.T, network *vnet.Network, bus *fake.Bus, base int) *testNode {
	t.Helper()

	clipboard := fake.NewClipboard()
	sockets := newSocketFactory(network, base)
	created, err := New(Config{
		DataDir:   t.TempDir(),
		Name:      fmt.Sprintf("device-%d", base),
		Platform:  model.PlatformMacOS,
		Keystore:  fake.NewKeystore(),
		Clipboard: clipboard,
		Discovery: bus.Discovery(),
		Logger:    testLogger(base),
		Limits: Limits{
			DialTimeout:       3 * time.Second,
			ReconnectDelay:    50 * time.Millisecond,
			ReconnectMaxDelay: 500 * time.Millisecond,
		},
		ListenPacket: sockets.listenPacket,
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	n := &testNode{Node: created, clipboard: clipboard, sockets: sockets}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := n.Stop(ctx); err != nil {
			t.Errorf("stop node: %v", err)
		}
	})
	return n
}

// start starts the node.
func (n *testNode) start(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("start node: %v", err)
	}
}

// listenAddr returns the address of the main listener.
func (n *testNode) listenAddr() string {
	return n.sockets.bound(0)
}

// dialerAddr returns the address of the dialer socket.
func (n *testNode) dialerAddr() string {
	return n.sockets.bound(1)
}

// subscribe returns a subscriber that the test closes.
func (n *testNode) subscribe(t *testing.T) *Subscriber {
	t.Helper()

	sub := n.Subscribe()
	t.Cleanup(sub.Close)
	return sub
}

// pair stores the other node as a Personal peer. The address is not set: the
// nodes have to find each other with discovery.
func (n *testNode) pair(t *testing.T, other *testNode) {
	t.Helper()

	err := n.AddPeer(context.Background(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     other.cfg.Name,
		Platform: model.PlatformMacOS,
		Role:     model.RolePersonal,
	})
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
}

// sessionAddrs returns the local and the remote address of the live session
// with the peer.
func (n *testNode) sessionAddrs(peer model.DeviceID) (local, remote string) {
	l := n.lookup(peer)
	if l == nil {
		return "", ""
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil || !l.online {
		return "", ""
	}
	return l.conn.LocalAddr().String(), l.conn.RemoteAddr().String()
}

// online reports whether the node has a session with the peer.
func (n *testNode) online(peer model.DeviceID) bool {
	local, _ := n.sessionAddrs(peer)
	return local != ""
}

// generation returns the generation of the live session with the peer, or zero
// when the peer is offline.
func (n *testNode) generation(peer model.DeviceID) uint64 {
	l := n.lookup(peer)
	if l == nil {
		return 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.online {
		return 0
	}
	return l.gen
}

// waitNewSession waits until the node runs a session with the peer that comes
// after the generation before.
func waitNewSession(t *testing.T, n *testNode, peer model.DeviceID, before uint64) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if generation := n.generation(peer); generation > before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	l := n.lookup(peer)
	if l == nil {
		t.Fatalf("the node has no link to %s", peer)
	}
	l.mu.Lock()
	state := fmt.Sprintf("gen %d, want above %d, online %v, conn %v, session %v, blocked %v, dialing %v, attempts %d",
		l.gen, before, l.online, l.conn != nil, l.sess != nil, l.blocked, l.dialing, l.attempts)
	l.mu.Unlock()
	t.Fatalf("the node did not open a new session with %s within %v (%s)", peer, testTimeout, state)
}

// dialBoth makes the two nodes dial each other at the same time. A connection
// that the other side replaces is closed during the handshake, so the errors of
// the attempts are expected.
func dialBoth(t *testing.T, a, b *testNode) {
	t.Helper()

	results := make(chan error, 2)
	for _, pair := range [][2]*testNode{{a, b}, {b, a}} {
		go func(from, to *testNode) {
			conn, err := from.dial(from.ctx, to.listenAddr())
			if err != nil {
				results <- err
				return
			}
			results <- from.attach(from.ctx, conn, false)
		}(pair[0], pair[1])
	}

	for range 2 {
		if err := <-results; err != nil {
			t.Logf("dial: %v", err)
		}
	}
}

// waitEvent waits for an event of the kind about the peer.
func waitEvent(t *testing.T, sub *Subscriber, kind EventKind, peer model.DeviceID) Event {
	t.Helper()

	timeout := time.After(testTimeout)
	for {
		select {
		case event, ok := <-sub.Events():
			if !ok {
				t.Fatalf("subscription closed while waiting for the %s event", kind)
			}
			if event.Kind == kind && (peer.IsZero() || event.Peer == peer) {
				return event
			}
		case <-timeout:
			t.Fatalf("no %s event for %s within %v", kind, peer, testTimeout)
		}
	}
}

// waitOnline waits until the node has a session with the peer.
func waitOnline(t *testing.T, n *testNode, peer model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if n.online(peer) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no session with %s within %v", peer, testTimeout)
}

// waitClipboard waits until the clipboard holds the plain text want.
func waitClipboard(t *testing.T, clipboard *fake.Clipboard, want string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if content, ok := clipboard.LastWrite(); ok {
			for _, representation := range content.Representations {
				if representation.MIME == clip.MIMETextPlain && string(representation.Data) == want {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the clipboard did not receive %q within %v", want, testTimeout)
}

// textSnapshot returns clipboard content with one plain text representation.
func textSnapshot(text string) ports.Snapshot {
	return ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
	}}
}

// checkCachedAddr checks that the node cached the listener address of the peer.
func checkCachedAddr(t *testing.T, n *testNode, peer *testNode) {
	t.Helper()

	record, err := n.Trust().Get(context.Background(), peer.DeviceID())
	if err != nil {
		t.Fatalf("read peer: %v", err)
	}
	if !slices.Contains(record.LastAddrs, peer.listenAddr()) {
		local, remote := n.sessionAddrs(peer.DeviceID())
		peerLocal, peerRemote := peer.sessionAddrs(n.DeviceID())
		t.Fatalf("cached addresses %v do not contain the listener address %s (sockets %v, session %s -> %s, peer sockets %v, peer session %s -> %s)",
			record.LastAddrs, peer.listenAddr(), n.sockets.boundAddrs(), local, remote,
			peer.sockets.boundAddrs(), peerLocal, peerRemote)
	}
}

// smaller returns the two nodes ordered by device ID.
func smaller(a, b *testNode) (small, large *testNode) {
	aID, bID := a.DeviceID(), b.DeviceID()
	if bytes.Compare(aID[:], bID[:]) < 0 {
		return a, b
	}
	return b, a
}
