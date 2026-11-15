package pairing

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// testTimeout bounds the wait for an event in the tests.
const testTimeout = 15 * time.Second

// testIdentity returns an identity derived from the seed.
func testIdentity(t *testing.T, seed byte) *identity.Identity {
	t.Helper()
	edSeed := bytes.Repeat([]byte{seed}, identity.SeedSize)
	pqSeed := bytes.Repeat([]byte{seed + 7}, identity.SeedSize)
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return id
}

// testEKM returns fixed exporter key material.
func testEKM() []byte {
	ekm := make([]byte, sasKeySize)
	for i := range ekm {
		ekm[i] = byte(i)
	}
	return ekm
}

// openTrust opens a trust store in a temporary database.
func openTrust(t *testing.T) *trust.Store {
	t.Helper()

	migrations, err := trust.Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	db, err := store.Open(context.Background(), store.Options{
		Dir:        t.TempDir(),
		Keystore:   fake.NewKeystore(),
		Migrations: migrations,
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	trustStore, err := trust.Open(db)
	if err != nil {
		t.Fatalf("open trust store: %v", err)
	}
	return trustStore
}

// testDiscovery adapts the fake discovery to the interface of the window.
type testDiscovery struct {
	*fake.Discovery
}

// StopService withdraws the announcement of the window.
func (d *testDiscovery) StopService(ports.Service) error {
	return d.Stop()
}

// socketFactory binds virtual packet connections on consecutive addresses.
type socketFactory struct {
	mu   sync.Mutex
	net  *vnet.Network
	next int
}

// newSockets returns a factory that hands out addresses from base.
func newSockets(network *vnet.Network, base int) *socketFactory {
	return &socketFactory{net: network, next: base}
}

// ListenPacket binds the next virtual address.
func (f *socketFactory) ListenPacket(context.Context) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(f.next))
	f.next++
	return f.net.ListenPacket(addr)
}

// testNode is one device of a pairing test.
type testNode struct {
	identity  *identity.Identity
	trust     *trust.Store
	discovery *testDiscovery
	sockets   *socketFactory
	name      string
	window    *Window
}

// newNode creates a device with its own trust store on the discovery bus.
func newNode(t *testing.T, network *vnet.Network, bus *fake.Bus, seed byte, base int) *testNode {
	t.Helper()
	return &testNode{
		identity:  testIdentity(t, seed),
		trust:     openTrust(t),
		discovery: &testDiscovery{Discovery: bus.Discovery()},
		sockets:   newSockets(network, base),
		name:      fmt.Sprintf("device-%d", seed),
	}
}

// open opens the pairing window of the node.
func (n *testNode) open(t *testing.T, clock ports.Clock, role model.Role) {
	t.Helper()

	window, err := Open(context.Background(), Options{
		Identity:     n.identity,
		Trust:        n.trust,
		Discovery:    n.discovery,
		Name:         n.name,
		Platform:     model.PlatformMacOS,
		Role:         role,
		Clock:        clock,
		ListenPacket: n.sockets.ListenPacket,
	})
	if err != nil {
		t.Fatalf("open window: %v", err)
	}
	t.Cleanup(func() { _ = window.Close() })
	n.window = window
}

// deviceID returns the device ID of the node.
func (n *testNode) deviceID() model.DeviceID {
	return n.identity.DeviceID()
}

// waitEvent returns the next event of the kind, skipping the others.
func waitEvent(t *testing.T, window *Window, kind EventKind) Event {
	t.Helper()
	timeout := time.After(testTimeout)
	for {
		select {
		case event, ok := <-window.Events():
			if !ok {
				t.Fatalf("window closed while waiting for the %s event", kind)
			}
			if event.Kind == kind {
				return event
			}
		case <-timeout:
			t.Fatalf("no %s event within %v", kind, testTimeout)
		}
	}
}

// waitCandidate waits until the window sees the candidate at addr.
func waitCandidate(t *testing.T, window *Window, addr string) {
	t.Helper()
	timeout := time.After(testTimeout)
	for {
		select {
		case event, ok := <-window.Events():
			if !ok {
				t.Fatal("window closed while waiting for a candidate")
			}
			if event.Kind == EventCandidate && event.Addr == addr {
				return
			}
		case <-timeout:
			t.Fatalf("candidate %s not found within %v", addr, testTimeout)
		}
	}
}

// waitDone waits until the window is closed.
func waitDone(t *testing.T, window *Window) {
	t.Helper()
	select {
	case <-window.Done():
	case <-time.After(testTimeout):
		t.Fatal("the window stayed open")
	}
}

// dial starts a pairing exchange in the background.
func dial(window *Window, addr string) <-chan error {
	errors := make(chan error, 1)
	go func() {
		errors <- window.Dial(context.Background(), addr)
	}()
	return errors
}

// newTestDialer returns a pairing dialer on the virtual network with a short
// handshake timeout. It reports the function that closes the dialer.
func newTestDialer(t *testing.T, network *vnet.Network, addr string) (*transport.Dialer, func()) {
	t.Helper()

	pc, err := network.ListenPacket(addr)
	if err != nil {
		t.Fatalf("packet connection %s: %v", addr, err)
	}
	dialer, err := transport.NewDialer(pc, testIdentity(t, 9), unpairedVerifier{}, transport.Config{
		HandshakeTimeout: time.Second,
	})
	if err != nil {
		_ = pc.Close()
		t.Fatalf("dialer: %v", err)
	}
	return dialer, func() { _ = dialer.Close() }
}
