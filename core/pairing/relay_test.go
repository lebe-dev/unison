package pairing

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// mockRelayHub routes datagrams between virtual mailbox connections in memory.
type mockRelayHub struct {
	mu      sync.Mutex
	boxes   map[string][]*mockMailboxConn
	pktSize int
}

func newMockRelayHub() *mockRelayHub {
	return &mockRelayHub{
		boxes:   make(map[string][]*mockMailboxConn),
		pktSize: transport.MinPacketSize + 100,
	}
}

func (h *mockRelayHub) open(key string) net.PacketConn {
	c := &mockMailboxConn{
		hub:        h,
		key:        key,
		addr:       mockRelayAddr(key),
		closed:     make(chan struct{}),
		signal:     make(chan struct{}, 1),
		deadlineCh: make(chan struct{}),
	}
	h.mu.Lock()
	h.boxes[key] = append(h.boxes[key], c)
	h.mu.Unlock()
	return c
}

func (h *mockRelayHub) send(from *mockMailboxConn, data []byte) {
	cp := append([]byte(nil), data...)
	h.mu.Lock()
	peers := append([]*mockMailboxConn(nil), h.boxes[from.key]...)
	h.mu.Unlock()
	for _, p := range peers {
		if p != from {
			p.push(cp)
		}
	}
}

func (h *mockRelayHub) remove(c *mockMailboxConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.boxes[c.key]
	for i, item := range list {
		if item == c {
			h.boxes[c.key] = append(list[:i], list[i+1:]...)
			break
		}
	}
}

type mockRelayClient struct {
	hub          *mockRelayHub
	token        string
	discoveryErr error
}

func (c *mockRelayClient) OpenDiscovery(context.Context) (net.PacketConn, error) {
	if c.discoveryErr != nil {
		return nil, c.discoveryErr
	}
	return c.hub.open("disc:" + c.token), nil
}

func (c *mockRelayClient) OpenSession(_ context.Context, initiator, responder [8]byte) (net.PacketConn, error) {
	return c.hub.open("sess:" + c.token + ":" + FormatRelayAddr(initiator) + ":" + FormatRelayAddr(responder)), nil
}

func (c *mockRelayClient) PacketSize() int {
	return c.hub.pktSize
}

type mockRelayAddr string

func (a mockRelayAddr) Network() string { return "relay" }
func (a mockRelayAddr) String() string  { return string(a) }

type mockMailboxConn struct {
	hub        *mockRelayHub
	key        string
	addr       mockRelayAddr
	closed     chan struct{}
	once       sync.Once
	signal     chan struct{}
	mu         sync.Mutex
	queue      [][]byte
	readDead   time.Time
	deadlineCh chan struct{}
}

func (c *mockMailboxConn) push(data []byte) {
	select {
	case <-c.closed:
		return
	default:
	}
	c.mu.Lock()
	c.queue = append(c.queue, data)
	c.mu.Unlock()
	select {
	case c.signal <- struct{}{}:
	default:
	}
}

func (c *mockMailboxConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			data := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return copy(p, data), c.addr, nil
		}
		deadline := c.readDead
		changed := c.deadlineCh
		c.mu.Unlock()

		select {
		case <-c.closed:
			return 0, nil, net.ErrClosed
		default:
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return 0, nil, os.ErrDeadlineExceeded
		}

		var expiry <-chan time.Time
		var timer *time.Timer
		if !deadline.IsZero() {
			timer = time.NewTimer(time.Until(deadline))
			expiry = timer.C
		}
		select {
		case <-c.signal:
		case <-c.closed:
		case <-changed:
		case <-expiry:
			timer.Stop()
			return 0, nil, os.ErrDeadlineExceeded
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (c *mockMailboxConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	c.hub.send(c, p)
	return len(p), nil
}

func (c *mockMailboxConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		c.hub.remove(c)
	})
	return nil
}

func (c *mockMailboxConn) LocalAddr() net.Addr { return c.addr }

func (c *mockMailboxConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

func (c *mockMailboxConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDead = t
	close(c.deadlineCh)
	c.deadlineCh = make(chan struct{})
	c.mu.Unlock()
	return nil
}

func (c *mockMailboxConn) SetWriteDeadline(time.Time) error { return nil }
func (c *mockMailboxConn) SetReadBuffer(int) error          { return nil }
func (c *mockMailboxConn) SetWriteBuffer(int) error         { return nil }

func TestRelayAddr(t *testing.T) {
	id := [8]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	formatted := FormatRelayAddr(id)
	if formatted != "relay:0123456789abcdef" {
		t.Fatalf("FormatRelayAddr = %q, want relay:0123456789abcdef", formatted)
	}
	if !IsRelayAddr(formatted) {
		t.Fatalf("IsRelayAddr(%q) = false, want true", formatted)
	}
	parsed, ok := ParseRelayAddr(formatted)
	if !ok || parsed != id {
		t.Fatalf("ParseRelayAddr(%q) = (%x, %v), want (%x, true)", formatted, parsed, ok, id)
	}

	invalid := []string{
		"",
		"relay:",
		"relay:0123456789abcde",
		"relay:0123456789abcdef0",
		"relay:0123456789ABCDEF",
		"relay:0123456789abcdeg",
		"127.0.0.1:1234",
		"other:0123456789abcdef",
	}
	for _, addr := range invalid {
		if IsRelayAddr(addr) {
			t.Errorf("IsRelayAddr(%q) = true, want false", addr)
		}
	}
}

func TestRelayDiscoveryAndSAS(t *testing.T) {
	hub := newMockRelayHub()
	// Separate local networks and discovery buses ensure only relay works.
	a := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1, 5100)
	b := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2, 5200)

	openWithRelay := func(n *testNode, token string) {
		t.Helper()
		w, err := Open(context.Background(), Options{
			Identity:            n.identity,
			Trust:               n.trust,
			Discovery:           n.discovery,
			Name:                n.name,
			Platform:            model.PlatformMacOS,
			Role:                model.RolePersonal,
			Clock:               ports.SystemClock,
			ListenPacket:        n.sockets.ListenPacket,
			Relay:               &mockRelayClient{hub: hub, token: token},
			RelayBeaconInterval: 50 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("open window: %v", err)
		}
		t.Cleanup(func() { _ = w.Close() })
		n.window = w
	}

	openWithRelay(a, "shared-token")
	openWithRelay(b, "shared-token")

	candA := waitEvent(t, a.window, EventCandidate)
	candB := waitEvent(t, b.window, EventCandidate)

	if !IsRelayAddr(candA.Addr) || candA.TXT["transport"] != "relay" {
		t.Fatalf("node A got unexpected candidate: %+v", candA)
	}
	if !IsRelayAddr(candB.Addr) || candB.TXT["transport"] != "relay" {
		t.Fatalf("node B got unexpected candidate: %+v", candB)
	}

	dialErr := dial(a.window, candA.Addr)

	reqA := waitEvent(t, a.window, EventRequest)
	reqB := waitEvent(t, b.window, EventRequest)
	if reqA.SAS.String() != reqB.SAS.String() {
		t.Fatalf("SAS mismatch: A=%q B=%q", reqA.SAS.String(), reqB.SAS.String())
	}

	if err := a.window.Confirm(context.Background(), reqA.DeviceID); err != nil {
		t.Fatalf("confirm A: %v", err)
	}
	if err := b.window.Confirm(context.Background(), reqB.DeviceID); err != nil {
		t.Fatalf("confirm B: %v", err)
	}

	waitEvent(t, a.window, EventPaired)
	waitEvent(t, b.window, EventPaired)

	if err := <-dialErr; err != nil {
		t.Fatalf("dial returned error: %v", err)
	}
	if _, err := a.trust.Get(context.Background(), b.deviceID()); err != nil {
		t.Fatalf("A did not store B: %v", err)
	}
	if _, err := b.trust.Get(context.Background(), a.deviceID()); err != nil {
		t.Fatalf("B did not store A: %v", err)
	}
}

func TestRelayDiscoveryErrorDoesNotBreakLocalPairing(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	a := newNode(t, network, bus, 3, 5300)
	b := newNode(t, network, bus, 4, 5400)

	wA, err := Open(context.Background(), Options{
		Identity:     a.identity,
		Trust:        a.trust,
		Discovery:    a.discovery,
		Name:         a.name,
		Platform:     model.PlatformMacOS,
		Role:         model.RolePersonal,
		ListenPacket: a.sockets.ListenPacket,
		Relay:        &mockRelayClient{hub: newMockRelayHub(), discoveryErr: errors.New("relay down")},
	})
	if err != nil {
		t.Fatalf("open window A: %v", err)
	}
	t.Cleanup(func() { _ = wA.Close() })
	a.window = wA

	b.open(t, ports.SystemClock, model.RolePersonal)
	waitCandidate(t, a.window, b.window.Addr().String())
}

func TestRelayDialRejectsSelfAndMissingRelay(t *testing.T) {
	a := newNode(t, vnet.NewNetwork(), fake.NewBus(), 5, 5500)
	a.open(t, ports.SystemClock, model.RolePersonal)

	other := FormatRelayAddr([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	if err := a.window.Dial(context.Background(), other); err == nil {
		t.Fatal("expected error dialing relay without Relay configured")
	}
	waitEvent(t, a.window, EventFailed)

	b := newNode(t, vnet.NewNetwork(), fake.NewBus(), 6, 5600)
	wB, err := Open(context.Background(), Options{
		Identity:     b.identity,
		Trust:        b.trust,
		Discovery:    b.discovery,
		Name:         b.name,
		Platform:     model.PlatformMacOS,
		Role:         model.RolePersonal,
		ListenPacket: b.sockets.ListenPacket,
		Relay:        &mockRelayClient{hub: newMockRelayHub(), token: "tok"},
	})
	if err != nil {
		t.Fatalf("open window B: %v", err)
	}
	t.Cleanup(func() { _ = wB.Close() })

	if err := wB.Dial(context.Background(), FormatRelayAddr(wB.relayID)); err == nil {
		t.Fatal("expected error dialing self over relay")
	}
	waitEvent(t, wB, EventFailed)
}

type countingRelayClient struct {
	*mockRelayClient
	mu           sync.Mutex
	sessionCalls int
}

func (c *countingRelayClient) OpenSession(ctx context.Context, initiator, responder [8]byte) (net.PacketConn, error) {
	c.mu.Lock()
	c.sessionCalls++
	c.mu.Unlock()
	return c.mockRelayClient.OpenSession(ctx, initiator, responder)
}

func (c *countingRelayClient) sessions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionCalls
}

func TestRelayDiscoveryCapsCandidates(t *testing.T) {
	hub := newMockRelayHub()
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 7, 5700)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))

	w, err := Open(context.Background(), Options{
		Identity:            n.identity,
		Trust:               n.trust,
		Discovery:           n.discovery,
		Name:                n.name,
		Platform:            model.PlatformMacOS,
		Role:                model.RolePersonal,
		Clock:               clock,
		ListenPacket:        n.sockets.ListenPacket,
		Relay:               &mockRelayClient{hub: hub, token: "cap-token"},
		RelayBeaconInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("open window: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Drain candidates concurrently so the event buffer never stalls the test.
	var (
		mu         sync.Mutex
		candidates int
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range w.Events() {
			if ev.Kind == EventCandidate {
				mu.Lock()
				candidates++
				mu.Unlock()
			}
		}
	}()

	attacker := hub.open("disc:cap-token")
	t.Cleanup(func() { _ = attacker.Close() })

	// Wait until the window has opened the discovery mailbox.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		ready := w.relayDiscovery != nil
		w.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	var msg [relayBeaconLen]byte
	copy(msg[:4], relayMagic[:])
	msg[4] = relayMsgBeacon
	for i := 1; i <= maxRelayCandidates+25; i++ {
		msg[5] = byte(i)
		msg[6] = byte(i >> 8)
		if _, err := attacker.WriteTo(msg[:], attacker.LocalAddr()); err != nil {
			t.Fatalf("send beacon %d: %v", i, err)
		}
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := candidates
		mu.Unlock()
		if got == maxRelayCandidates {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Give any excess datagrams time to be processed.
	time.Sleep(50 * time.Millisecond)

	w.mu.Lock()
	seenCount := len(w.relaySeen)
	w.mu.Unlock()
	if seenCount != maxRelayCandidates {
		t.Fatalf("relaySeen = %d, want %d", seenCount, maxRelayCandidates)
	}
	mu.Lock()
	gotCandidates := candidates
	mu.Unlock()
	if gotCandidates != maxRelayCandidates {
		t.Fatalf("candidates emitted = %d, want %d", gotCandidates, maxRelayCandidates)
	}

	_ = w.Close()
	<-done
}

func TestRelayInviteRateLimitAndReadyThrottle(t *testing.T) {
	hub := newMockRelayHub()
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 8, 5800)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))
	relayClient := &countingRelayClient{mockRelayClient: &mockRelayClient{hub: hub, token: "invite-token"}}

	w, err := Open(context.Background(), Options{
		Identity:            n.identity,
		Trust:               n.trust,
		Discovery:           n.discovery,
		Name:                n.name,
		Platform:            model.PlatformMacOS,
		Role:                model.RolePersonal,
		Clock:               clock,
		ListenPacket:        n.sockets.ListenPacket,
		Config:              transport.Config{HandshakeTimeout: 2 * time.Second},
		Relay:               relayClient,
		RelayBeaconInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("open window: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	attacker := hub.open("disc:invite-token")
	t.Cleanup(func() { _ = attacker.Close() })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		ready := w.relayDiscovery != nil
		w.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	sendInvite := func(initiator [8]byte) {
		var msg [relaySignalLen]byte
		copy(msg[:4], relayMagic[:])
		msg[4] = relayMsgInvite
		copy(msg[5:13], initiator[:])
		copy(msg[13:21], w.relayID[:])
		if _, err := attacker.WriteTo(msg[:], attacker.LocalAddr()); err != nil {
			t.Fatalf("send invite: %v", err)
		}
	}

	// Send more unique invites than maxAttempts within the same instant.
	for i := 1; i <= maxAttempts+4; i++ {
		sendInvite([8]byte{byte(i), 0xaa})
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && relayClient.sessions() < maxAttempts {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := relayClient.sessions(); got != maxAttempts {
		t.Fatalf("OpenSession calls = %d, want %d", got, maxAttempts)
	}

	// Drain initial messages, then flood duplicate invites for an active
	// initiator without advancing the clock: Ready replies must be throttled.
	_ = attacker.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 64)
	for {
		if _, _, err := attacker.ReadFrom(buf); err != nil {
			break
		}
	}
	for range 10 {
		sendInvite([8]byte{1, 0xaa})
	}
	readyCount := 0
	_ = attacker.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	for {
		m, _, err := attacker.ReadFrom(buf)
		if err != nil {
			break
		}
		if m == relaySignalLen && buf[4] == relayMsgReady {
			readyCount++
		}
	}
	if readyCount != 0 {
		t.Fatalf("throttled duplicate invites elicited %d Ready replies, want 0", readyCount)
	}
}

func TestRelayConcurrentDialDeduplicated(t *testing.T) {
	hub := newMockRelayHub()
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 9, 5900)

	w, err := Open(context.Background(), Options{
		Identity:            n.identity,
		Trust:               n.trust,
		Discovery:           n.discovery,
		Name:                n.name,
		Platform:            model.PlatformMacOS,
		Role:                model.RolePersonal,
		Clock:               ports.SystemClock,
		ListenPacket:        n.sockets.ListenPacket,
		Config:              transport.Config{HandshakeTimeout: 2 * time.Second},
		Relay:               &mockRelayClient{hub: hub, token: "dedup-token"},
		RelayBeaconInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("open window: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	targetID := [8]byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04}
	target := FormatRelayAddr(targetID)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	err1 := make(chan error, 1)
	go func() {
		err1 <- w.Dial(ctx1, target)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		_, dialing := w.relayDials[targetID]
		w.mu.Unlock()
		if dialing {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := w.Dial(context.Background(), target); !errors.Is(err, ErrPending) {
		t.Fatalf("second concurrent Dial = %v, want ErrPending", err)
	}

	cancel1()
	if err := <-err1; err == nil {
		t.Fatal("expected canceled first dial to fail")
	}
}
