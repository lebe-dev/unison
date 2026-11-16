package nodetest

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// Harness defaults.
const (
	// DefaultTimeout bounds every wait of the harness.
	DefaultTimeout = 15 * time.Second
	// clockStep is how much the virtual clock advances per tick.
	clockStep = 5 * time.Millisecond
	// firstPort is the port of the first virtual address.
	firstPort = 20000
)

// defaultTransport keeps the keepalive and the idle timeout short, so a link
// failure shows up within a few seconds.
func defaultTransport() transport.Config {
	return transport.Config{
		KeepAlive:        250 * time.Millisecond,
		IdleTimeout:      time.Second,
		HandshakeTimeout: 2 * time.Second,
	}
}

// defaultLimits are the connection settings of a harness node.
func defaultLimits() node.Limits {
	return node.Limits{
		DialTimeout:       3 * time.Second,
		ReconnectDelay:    25 * time.Millisecond,
		ReconnectMaxDelay: 250 * time.Millisecond,
	}
}

// Option tunes a Harness.
type Option func(*settings)

// settings are the resolved options of a Harness.
type settings struct {
	timeout   time.Duration
	loss      float64
	latency   time.Duration
	logger    *slog.Logger
	transport transport.Config
	limits    node.Limits
}

// WithTimeout bounds every wait of the harness.
func WithTimeout(d time.Duration) Option {
	return func(s *settings) { s.timeout = d }
}

// WithLoss drops the given share of the datagrams of the harness network.
func WithLoss(p float64) Option {
	return func(s *settings) { s.loss = p }
}

// WithLatency delays every datagram of the harness network by d.
func WithLatency(d time.Duration) Option {
	return func(s *settings) { s.latency = d }
}

// WithLogger makes the nodes log to log.
func WithLogger(log *slog.Logger) Option {
	return func(s *settings) { s.logger = log }
}

// WithTransport overrides the QUIC settings of the nodes. A zero field keeps the
// harness default.
func WithTransport(cfg transport.Config) Option {
	return func(s *settings) {
		if cfg.KeepAlive > 0 {
			s.transport.KeepAlive = cfg.KeepAlive
		}
		if cfg.IdleTimeout > 0 {
			s.transport.IdleTimeout = cfg.IdleTimeout
		}
		if cfg.HandshakeTimeout > 0 {
			s.transport.HandshakeTimeout = cfg.HandshakeTimeout
		}
		if cfg.MaxIncomingStreams > 0 {
			s.transport.MaxIncomingStreams = cfg.MaxIncomingStreams
		}
		if cfg.MaxIncomingUniStreams > 0 {
			s.transport.MaxIncomingUniStreams = cfg.MaxIncomingUniStreams
		}
	}
}

// WithLimits overrides the connection manager settings of the nodes. A zero
// field keeps the harness default.
func WithLimits(limits node.Limits) Option {
	return func(s *settings) {
		if limits.DialTimeout > 0 {
			s.limits.DialTimeout = limits.DialTimeout
		}
		if limits.ReconnectDelay > 0 {
			s.limits.ReconnectDelay = limits.ReconnectDelay
		}
		if limits.ReconnectMaxDelay > 0 {
			s.limits.ReconnectMaxDelay = limits.ReconnectMaxDelay
		}
		if limits.EventBuffer > 0 {
			s.limits.EventBuffer = limits.EventBuffer
		}
	}
}

// Harness runs nodes on one virtual network with a shared discovery bus and a
// shared virtual clock. Create it with New and add the nodes with NewNode.
type Harness struct {
	t     testing.TB
	net   *vnet.Network
	bus   *fake.Bus
	clock *virtualClock
	ports *portAllocator

	timeout   time.Duration
	log       *slog.Logger
	transport transport.Config
	limits    node.Limits

	mu    sync.Mutex
	nodes []*Node
}

// New starts a harness and releases it when the test ends.
func New(t testing.TB, opts ...Option) *Harness {
	t.Helper()

	cfg := settings{
		timeout:   DefaultTimeout,
		transport: defaultTransport(),
		limits:    defaultLimits(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.timeout <= 0 {
		cfg.timeout = DefaultTimeout
	}

	network := vnet.NewNetwork()
	if err := network.SetLoss(cfg.loss); err != nil {
		t.Fatalf("set network loss: %v", err)
	}
	if err := network.SetLatency(cfg.latency); err != nil {
		t.Fatalf("set network latency: %v", err)
	}

	h := &Harness{
		t:         t,
		net:       network,
		bus:       fake.NewBus(),
		clock:     newVirtualClock(time.Now(), clockStep),
		ports:     newPortAllocator(firstPort),
		timeout:   cfg.timeout,
		log:       cfg.logger,
		transport: cfg.transport,
		limits:    cfg.limits,
	}
	t.Cleanup(h.close)
	return h
}

// close releases the clock and the network. The nodes unregister their cleanup
// later, so they are stopped by now.
func (h *Harness) close() {
	h.clock.close()
	if err := h.net.Close(); err != nil {
		h.t.Errorf("close the network: %v", err)
	}
}

// NewNode starts a node named name and stops it when the test ends.
func (h *Harness) NewNode(name string) *Node {
	h.t.Helper()

	clipboard := fake.NewClipboard()
	sockets := newSockets(h.net, h.ports)
	host, err := node.New(node.Config{
		DataDir:      h.t.TempDir(),
		Name:         name,
		Platform:     model.PlatformMacOS,
		Keystore:     fake.NewKeystore(),
		Clipboard:    clipboard,
		Discovery:    h.bus.Discovery(),
		Clock:        h.clock,
		Logger:       h.log,
		Limits:       h.limits,
		Transport:    h.transport,
		ListenPacket: sockets.listenPacket,
	})
	if err != nil {
		h.t.Fatalf("create node %q: %v", name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	if err := host.Start(ctx); err != nil {
		h.t.Fatalf("start node %q: %v", name, err)
	}

	n := &Node{
		Node:      host,
		harness:   h,
		name:      name,
		platform:  model.PlatformMacOS,
		clipboard: clipboard,
		sockets:   sockets,
		sessions:  newSessionLog(),
		notify:    make(chan struct{}),
		done:      make(chan struct{}),
	}
	if err := host.AddSessionHook(n.trackSession); err != nil {
		h.t.Fatalf("node %q: register the session hook: %v", name, err)
	}
	n.sub = host.Subscribe()
	go n.record()
	h.t.Cleanup(n.close)

	h.mu.Lock()
	h.nodes = append(h.nodes, n)
	h.mu.Unlock()
	return n
}

// Nodes returns the nodes created by the harness.
func (h *Harness) Nodes() []*Node {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*Node(nil), h.nodes...)
}

// Timeout returns the wait bound of the harness.
func (h *Harness) Timeout() time.Duration {
	return h.timeout
}

// Now returns the current time of the virtual clock.
func (h *Harness) Now() time.Time {
	return h.clock.Now()
}

// Advance moves the virtual clock forward, which fires the timers that became
// due. The clock also advances by itself, so this only skips extra time.
func (h *Harness) Advance(d time.Duration) {
	h.clock.Advance(d)
}

// SetLoss sets the probability of dropping a datagram.
func (h *Harness) SetLoss(p float64) {
	h.t.Helper()
	if err := h.net.SetLoss(p); err != nil {
		h.t.Fatalf("set network loss: %v", err)
	}
}

// SetLatency sets the delivery delay of every datagram.
func (h *Harness) SetLatency(d time.Duration) {
	h.t.Helper()
	if err := h.net.SetLatency(d); err != nil {
		h.t.Fatalf("set network latency: %v", err)
	}
}

// SetSeed reseeds the packet loss generator, which makes a lossy test
// reproducible.
func (h *Harness) SetSeed(seed uint64) {
	h.net.SetSeed(seed)
}

// Pair stores the two devices as Personal peers of each other, which is what a
// completed pairing leaves behind, and waits until they are connected. Use
// PairSAS to run the pairing exchange itself.
func (h *Harness) Pair(a, b *Node) {
	h.t.Helper()

	h.addPeer(a, b)
	h.addPeer(b, a)
	a.WaitOnline(b)
	b.WaitOnline(a)
}

// Offline takes the device off the network and waits until the device and its
// peers notice the broken links.
func (h *Harness) Offline(device *Node, peers ...*Node) {
	h.t.Helper()

	device.Offline()
	for _, peer := range peers {
		peer.WaitOffline(device)
		device.WaitOffline(peer)
	}
}

// Online brings the device back to the network and waits until it is connected
// to the peers again.
func (h *Harness) Online(device *Node, peers ...*Node) {
	h.t.Helper()

	device.Online()
	for _, peer := range peers {
		device.WaitOnline(peer)
		peer.WaitOnline(device)
	}
}

// ChangeAddress takes the device off the network, moves its sockets to new
// addresses and brings it back. It waits until the links are gone and the
// devices are connected on the new addresses.
func (h *Harness) ChangeAddress(device *Node, peers ...*Node) {
	h.t.Helper()

	h.Offline(device, peers...)
	device.MoveAddress()
	h.Online(device, peers...)
}

// Settle waits until every device is connected to every other device of the
// harness and the connections stay in place for the settle delay. Pairing
// several devices changes the connections of the devices that are already
// paired, so a test that builds a mesh calls this once before it starts.
func (h *Harness) Settle() {
	h.t.Helper()

	deadline := time.Now().Add(h.timeout)
	for {
		if h.settled() {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("the connections did not settle within %v: %s", h.timeout, h.describe())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// settled reports whether every device holds a settled session with every other
// device.
func (h *Harness) settled() bool {
	ctx := context.Background()
	nodes := h.Nodes()
	for _, n := range nodes {
		for _, peer := range nodes {
			if n == peer {
				continue
			}
			state, err := n.Peer(ctx, peer.DeviceID())
			if err != nil || !state.Online {
				return false
			}
			if !n.sessions.settled(peer.DeviceID(), settleDelay) {
				return false
			}
		}
	}
	return true
}

// describe returns the state of every device, which makes a failed wait easier
// to read.
func (h *Harness) describe() string {
	nodes := h.Nodes()
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		parts = append(parts, n.name+": "+n.sessions.String())
	}
	return strings.Join(parts, "; ")
}

// addPeer writes the other device into the trust store of from.
func (h *Harness) addPeer(from, to *Node) {
	h.t.Helper()

	peer := trust.Peer{
		Public:   to.Identity().Public(),
		Name:     to.name,
		Platform: to.platform,
		Role:     model.RolePersonal,
	}
	if err := from.AddPeer(context.Background(), peer); err != nil {
		h.t.Fatalf("node %s: add peer %s: %v", from.name, to.name, err)
	}
}

// PairSAS pairs the two devices through the pairing exchange: both open a
// window, one of them dials the other and both confirm the same short
// authentication string. It waits until the devices are connected.
func (h *Harness) PairSAS(a, b *Node) {
	h.t.Helper()

	for _, n := range []*Node{a, b} {
		if err := n.OpenPairing(model.RolePersonal); err != nil {
			h.t.Fatalf("node %s: open the pairing window: %v", n.name, err)
		}
	}

	candidate := a.WaitEvent(node.EventPairingCandidate, model.DeviceID{})
	if err := a.StartPairing(candidate.Addr); err != nil {
		h.t.Fatalf("node %s: start pairing: %v", a.name, err)
	}

	requestA := a.WaitEvent(node.EventPairingRequest, model.DeviceID{})
	requestB := b.WaitEvent(node.EventPairingRequest, model.DeviceID{})
	if requestA.Peer != b.DeviceID() || requestB.Peer != a.DeviceID() {
		h.t.Fatalf("the devices paired with the wrong peers: %s and %s",
			requestA.Peer, requestB.Peer)
	}
	if requestA.SAS.String() != requestB.SAS.String() {
		h.t.Fatalf("the devices show different strings: %s and %s",
			requestA.SAS, requestB.SAS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	if err := a.ConfirmPairing(ctx, requestA.Peer); err != nil {
		h.t.Fatalf("node %s: confirm the pairing: %v", a.name, err)
	}
	if err := b.ConfirmPairing(ctx, requestB.Peer); err != nil {
		h.t.Fatalf("node %s: confirm the pairing: %v", b.name, err)
	}

	a.WaitEvent(node.EventPairingPaired, b.DeviceID())
	b.WaitEvent(node.EventPairingPaired, a.DeviceID())
	a.WaitOnline(b)
	b.WaitOnline(a)
}

// DialAsStranger makes one connection attempt to target with an identity that
// no device trusts and returns its error. A device outside a pairing window has
// to refuse the connection during the handshake.
func (h *Harness) DialAsStranger(target *Node) error {
	h.t.Helper()

	stranger, err := strangerIdentity()
	if err != nil {
		h.t.Fatalf("create the stranger identity: %v", err)
	}

	pc, err := h.net.ListenPacket(h.ports.addr())
	if err != nil {
		h.t.Fatalf("bind the stranger socket: %v", err)
	}
	dialer, err := transport.NewDialer(pc, stranger, fixedPeer{peer: transport.Peer{
		Public: target.Identity().Public(),
		Role:   model.RolePersonal,
	}}, transport.Config{HandshakeTimeout: h.transport.HandshakeTimeout})
	if err != nil {
		_ = pc.Close()
		h.t.Fatalf("create the stranger dialer: %v", err)
	}
	defer func() { _ = dialer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	conn, err := dialer.Dial(ctx, vnet.NewAddr(target.ListenAddr()))
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// strangerIdentity returns a fresh identity.
func strangerIdentity() (*identity.Identity, error) {
	edSeed := make([]byte, identity.SeedSize)
	mldsaSeed := make([]byte, identity.SeedSize)
	if _, err := rand.Read(edSeed); err != nil {
		return nil, err
	}
	if _, err := rand.Read(mldsaSeed); err != nil {
		return nil, err
	}
	return identity.FromSeeds(edSeed, mldsaSeed)
}

// fixedPeer accepts any certificate and returns the peer it is given. It stands
// for the identity a dialer expects on the other side.
type fixedPeer struct {
	peer transport.Peer
}

// VerifyPeer returns the fixed peer.
func (v fixedPeer) VerifyPeer(context.Context, *x509.Certificate) (transport.Peer, error) {
	return v.peer, nil
}
