package node

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/gossip"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/trust"
)

// Connection management defaults.
const (
	// DefaultDialTimeout bounds one connection attempt.
	DefaultDialTimeout = 10 * time.Second
	// DefaultReconnectDelay is the first delay between connection attempts to a
	// peer.
	DefaultReconnectDelay = time.Second
	// DefaultReconnectMaxDelay caps the delay between connection attempts.
	DefaultReconnectMaxDelay = 30 * time.Second
	// DefaultEventBuffer is the event queue of one subscriber.
	DefaultEventBuffer = 64
	// maxCandidates bounds the cached discovery candidates.
	maxCandidates = 64
	// candidateTTL is how long a discovered address stays usable.
	candidateTTL = 10 * time.Minute
	// maxAddrLength bounds one address taken from discovery.
	maxAddrLength = 255
)

// Errors reported by the node.
var (
	// ErrStarted reports a second call to Start.
	ErrStarted = errors.New("node: already started")
	// ErrStopped reports a node that is not running.
	ErrStopped = errors.New("node: not running")
	// ErrPairingOpen reports a second pairing window.
	ErrPairingOpen = errors.New("node: pairing window is already open")
	// ErrPairingClosed reports a pairing action without an open window.
	ErrPairingClosed = errors.New("node: pairing window is closed")
)

// ListenPacketFunc binds the packet connection of one socket. port is the port
// of the main listener and zero asks for an ephemeral port.
type ListenPacketFunc func(ctx context.Context, port int) (net.PacketConn, error)

// Limits tunes the connection manager.
type Limits struct {
	// DialTimeout bounds one connection attempt. DefaultDialTimeout is used
	// when zero.
	DialTimeout time.Duration
	// ReconnectDelay is the first delay between connection attempts to a peer.
	// It doubles with every failed attempt up to ReconnectMaxDelay.
	// DefaultReconnectDelay is used when zero.
	ReconnectDelay time.Duration
	// ReconnectMaxDelay caps the delay between connection attempts.
	// DefaultReconnectMaxDelay is used when zero.
	ReconnectMaxDelay time.Duration
	// EventBuffer is the event queue of one subscriber. DefaultEventBuffer is
	// used when zero.
	EventBuffer int
}

// withDefaults fills the zero fields with the package defaults.
func (l Limits) withDefaults() Limits {
	if l.DialTimeout <= 0 {
		l.DialTimeout = DefaultDialTimeout
	}
	if l.ReconnectDelay <= 0 {
		l.ReconnectDelay = DefaultReconnectDelay
	}
	if l.ReconnectMaxDelay <= 0 {
		l.ReconnectMaxDelay = DefaultReconnectMaxDelay
	}
	if l.ReconnectMaxDelay < l.ReconnectDelay {
		l.ReconnectMaxDelay = l.ReconnectDelay
	}
	if l.EventBuffer <= 0 {
		l.EventBuffer = DefaultEventBuffer
	}
	return l
}

// Config describes a node. The platform ports are supplied by the caller, the
// rest is owned by the node.
type Config struct {
	// DataDir is the directory of the database and other node data.
	DataDir string
	// Name is the device name announced to peers.
	Name string
	// Platform is the operating system of this device.
	Platform model.Platform
	// Capabilities are the optional features announced in the handshake.
	Capabilities []string
	// Keystore keeps the identity secrets and the data encryption key.
	Keystore ports.Keystore
	// Clipboard is the local clipboard.
	Clipboard ports.Clipboard
	// Discovery announces this device and finds the devices of the local
	// network.
	Discovery ports.Discovery
	// Clock is the time source of the node. The system clock is used when nil.
	Clock ports.Clock
	// Logger receives node events. Nothing is logged when nil.
	Logger *slog.Logger
	// Limits tunes the connection manager.
	Limits Limits
	// Transport tunes the QUIC connections.
	Transport transport.Config
	// BlobLimit is the size limit of the blob store in bytes. The package
	// default is used when zero and the stored setting overrides it.
	BlobLimit int64
	// ListenPacket binds the packet connections. A UDP socket is bound when
	// nil.
	ListenPacket ListenPacketFunc
}

// SessionHook registers handlers on a new session. Hooks run before the
// handshake, while the session still accepts handlers.
type SessionHook func(ctx context.Context, sess *session.Session) error

// state is the lifecycle state of a Node.
type state uint8

// Node lifecycle states.
const (
	stateNew state = iota
	stateRunning
	stateStopped
)

// candidate is one discovered address of the local network.
type candidate struct {
	seen time.Time
}

// Node is a running device. Create it with New, start it with Start and release
// it with Stop.
type Node struct {
	cfg    Config
	limits Limits
	clock  ports.Clock
	log    *slog.Logger

	identity *identity.Identity
	db       *store.Store
	trust    *trust.Store
	clip     *clip.Pipeline
	policy   *policy.Decider
	blobs    *blob.Store
	history  *history.Store

	blobService *transfer.Service
	fetcher     *transfer.Fetcher

	gossip *gossip.Service

	listener *transport.Listener
	dialer   *transport.Dialer

	events *eventBus

	mu         sync.Mutex
	state      state
	ctx        context.Context
	cancel     context.CancelFunc
	links      map[model.DeviceID]*link
	candidates map[string]candidate
	probing    map[string]struct{}
	hooks      []SessionHook
	pairing    *pairingWindow
	network    model.NetworkState

	// addrMu serializes the read-modify-write cycles on the cached addresses.
	addrMu sync.Mutex

	// life serializes Start and Stop.
	life sync.Mutex

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopErr  error
}

// New validates the configuration and returns a node that is not started yet.
func New(cfg Config) (*Node, error) {
	switch {
	case cfg.DataDir == "":
		return nil, errors.New("node: data directory is required")
	case cfg.Name == "":
		return nil, errors.New("node: device name is required")
	case cfg.Platform == model.PlatformUnknown:
		return nil, errors.New("node: platform is required")
	case cfg.Keystore == nil:
		return nil, errors.New("node: keystore is required")
	case cfg.Clipboard == nil:
		return nil, errors.New("node: clipboard is required")
	case cfg.Discovery == nil:
		return nil, errors.New("node: discovery is required")
	}

	clock := cfg.Clock
	if clock == nil {
		clock = ports.SystemClock
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	listen := cfg.ListenPacket
	if listen == nil {
		listen = listenUDP
	}
	cfg.ListenPacket = listen
	cfg.Capabilities = append([]string(nil), cfg.Capabilities...)
	limits := cfg.Limits.withDefaults()

	n := &Node{
		cfg:        cfg,
		limits:     limits,
		clock:      clock,
		log:        log,
		events:     newEventBus(limits.EventBuffer, log),
		links:      make(map[model.DeviceID]*link),
		probing:    make(map[string]struct{}),
		candidates: make(map[string]candidate),
		// A device that has not reported its network is treated as online:
		// the desktop daemon has no network monitor of its own.
		network: model.NetworkState{Online: true},
	}
	n.hooks = []SessionHook{n.installClip, n.installTransfer, n.installHistory, n.installGossip}
	return n, nil
}

// Start opens the node: it opens the database and the identity, binds the
// sockets, announces the device, starts the clipboard pipeline and connects to
// every paired device. The context bounds the startup only, the node runs until
// Stop.
func (n *Node) Start(ctx context.Context) error {
	n.life.Lock()
	defer n.life.Unlock()

	n.mu.Lock()
	switch n.state {
	case stateRunning:
		n.mu.Unlock()
		return ErrStarted
	case stateStopped:
		n.mu.Unlock()
		return ErrStopped
	}
	n.state = stateRunning
	n.ctx, n.cancel = context.WithCancel(context.Background())
	n.mu.Unlock()

	if err := n.open(ctx); err != nil {
		_ = n.stop(context.Background())
		return err
	}
	n.run()
	return nil
}

// Stop ends every session, withdraws the announcements and closes the database.
// It is safe to call more than once.
func (n *Node) Stop(ctx context.Context) error {
	n.life.Lock()
	defer n.life.Unlock()
	return n.stop(ctx)
}

// Subscribe returns a subscriber for the node events. Close it to release the
// queue.
func (n *Node) Subscribe() *Subscriber {
	return n.events.subscribe()
}

// publish delivers one event to the subscribers.
func (n *Node) publish(event Event) {
	n.events.publish(event)
}

// AddSessionHook registers a hook that prepares every new session. The
// registration of handlers belongs here: it has to happen before the handshake.
func (n *Node) AddSessionHook(hook SessionHook) error {
	if hook == nil {
		return errors.New("node: nil session hook")
	}
	n.mu.Lock()
	n.hooks = append(n.hooks, hook)
	n.mu.Unlock()
	return nil
}

// DeviceID returns the device ID of this node. It is the zero value before
// Start.
func (n *Node) DeviceID() model.DeviceID {
	if n.identity == nil {
		return model.DeviceID{}
	}
	return n.identity.DeviceID()
}

// Identity returns the identity of this node. It is nil before Start.
func (n *Node) Identity() *identity.Identity {
	return n.identity
}

// Addr returns the address of the main listener. It is nil before Start.
func (n *Node) Addr() net.Addr {
	if n.listener == nil {
		return nil
	}
	return n.listener.Addr()
}

// Store returns the database of the node. It is nil before Start.
func (n *Node) Store() *store.Store {
	return n.db
}

// Trust returns the trust store of the node. It is nil before Start.
func (n *Node) Trust() *trust.Store {
	return n.trust
}

// Clip returns the clipboard pipeline of the node. It is nil before Start.
func (n *Node) Clip() *clip.Pipeline {
	return n.clip
}

// Network returns the last network state reported to the node.
func (n *Node) Network() model.NetworkState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.network
}

// AddPeer stores a paired device and connects to it.
func (n *Node) AddPeer(ctx context.Context, peer trust.Peer) error {
	if err := n.usable(); err != nil {
		return err
	}
	if err := n.trust.Add(ctx, peer); err != nil {
		return err
	}
	n.startLink(peer).retry()
	n.probeCandidates(n.ctx)
	return nil
}

// SetPeerAddress stores a manually entered address of a paired device and tries
// to connect to it.
func (n *Node) SetPeerAddress(ctx context.Context, id model.DeviceID, addr string) error {
	if err := n.usable(); err != nil {
		return err
	}
	if addr == "" {
		return errors.New("node: empty peer address")
	}
	if len(addr) > maxAddrLength {
		return errors.New("node: peer address is too long")
	}
	peer, err := n.trust.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := n.rememberAddr(ctx, id, addr, true); err != nil {
		return err
	}
	n.startLink(peer).retry()
	n.probeCandidates(n.ctx)
	return nil
}

// Peers returns the live state of every paired device, in the order the devices
// were added.
func (n *Node) Peers(ctx context.Context) ([]model.PeerState, error) {
	if err := n.usable(); err != nil {
		return nil, err
	}
	peers, err := n.trust.List(ctx, trust.ListFilter{})
	if err != nil {
		return nil, err
	}
	states := make([]model.PeerState, 0, len(peers))
	for _, peer := range peers {
		states = append(states, n.peerState(peer.DeviceID()))
	}
	return states, nil
}

// Peer returns the live state of one paired device.
func (n *Node) Peer(ctx context.Context, id model.DeviceID) (model.PeerState, error) {
	if err := n.usable(); err != nil {
		return model.PeerState{}, err
	}
	if _, err := n.trust.Get(ctx, id); err != nil {
		return model.PeerState{}, err
	}
	return n.peerState(id), nil
}

// NetworkChanged reports the network conditions of the device. The node
// announces itself again and retries every peer that is offline or waitlisted.
func (n *Node) NetworkChanged(state model.NetworkState) {
	n.mu.Lock()
	n.network = state
	running := n.state == stateRunning
	ctx := n.ctx
	n.mu.Unlock()

	if !running || ctx.Err() != nil {
		n.log.Debug("network change ignored", "online", state.Online)
		return
	}
	if !state.Online {
		n.log.Debug("network is offline")
		return
	}

	n.advertise(ctx)
	for _, l := range n.linkList() {
		l.retry()
	}
	n.probeCandidates(ctx)
}

// open creates the resources of the node.
func (n *Node) open(ctx context.Context) error {
	migrations, err := migrationSet()
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, store.Options{
		Dir:        n.cfg.DataDir,
		Keystore:   n.cfg.Keystore,
		Migrations: migrations,
	})
	if err != nil {
		return err
	}
	n.db = db

	id, err := identity.LoadOrCreate(ctx, n.cfg.Keystore)
	if err != nil {
		return err
	}
	n.identity = id

	trustStore, err := trust.Open(db)
	if err != nil {
		return err
	}
	n.trust = trustStore

	gossipService, err := gossip.New(n.ctx, gossip.Config{
		Store:                 trustStore,
		Local:                 id.DeviceID(),
		Logger:                n.log,
		OnPeersChanged:        n.gossipPeersChanged,
		OnDiscoveryKeyChanged: n.gossipGroupChanged,
	})
	if err != nil {
		return err
	}
	n.gossip = gossipService

	port, err := transport.MainPort(ctx, db.Settings())
	if err != nil {
		return err
	}
	listener, err := n.listenListener(ctx, port)
	if err != nil {
		return err
	}
	n.listener = listener

	dialer, err := n.listenDialer(ctx)
	if err != nil {
		return err
	}
	n.dialer = dialer

	if err := n.openBlobs(db); err != nil {
		return err
	}
	decider, err := n.openPolicy(ctx, db)
	if err != nil {
		return err
	}
	n.policy = decider

	ttls, err := history.LoadTTLs(ctx, db.Settings())
	if err != nil {
		n.log.Warn("reading the history lifetimes failed, using the defaults", "error", err)
	}
	hist, err := n.openHistory(db, id.DeviceID())
	if err != nil {
		return err
	}

	pipeline, err := clip.New(clip.Options{
		Clipboard: n.cfg.Clipboard,
		Store:     db,
		Origin:    id.DeviceID(),
		Announcer: n,
		Planner:   policyPlanner{decider: decider},
		History:   hist,
		Payloads:  blobPayloads{blobs: n.blobs},
		Fetch:     clip.FetchFunc(n.fetchPayload),
		Events:    n,
		Clock:     n.clock,
		Logger:    n.log,
		TTLs:      clip.TTLs{Text: ttls.Text, Image: ttls.Image, Files: ttls.Files},
	})
	if err != nil {
		return err
	}
	n.clip = pipeline
	return nil
}

// migrationSet collects the schema migrations of the packages the node owns.
func migrationSet() ([]store.Migration, error) {
	trustMigrations, err := trust.Migrations()
	if err != nil {
		return nil, err
	}
	clipMigrations, err := clip.Migrations()
	if err != nil {
		return nil, err
	}
	blobMigrations, err := blob.Migrations()
	if err != nil {
		return nil, err
	}
	historyMigrations, err := history.Migrations()
	if err != nil {
		return nil, err
	}

	migrations := make([]store.Migration, 0,
		len(trustMigrations)+len(clipMigrations)+len(blobMigrations)+len(historyMigrations))
	migrations = append(migrations, trustMigrations...)
	migrations = append(migrations, clipMigrations...)
	migrations = append(migrations, blobMigrations...)
	return append(migrations, historyMigrations...), nil
}

// listenListener binds the packet connection of the main listener.
func (n *Node) listenListener(ctx context.Context, port int) (*transport.Listener, error) {
	pc, err := n.cfg.ListenPacket(ctx, port)
	if err != nil {
		return nil, err
	}
	listener, err := transport.NewListener(pc, n.identity, peerVerifier{trust: n.trust}, n.cfg.Transport)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	return listener, nil
}

// listenDialer binds the packet connection of the dialer.
func (n *Node) listenDialer(ctx context.Context) (*transport.Dialer, error) {
	pc, err := n.cfg.ListenPacket(ctx, 0)
	if err != nil {
		return nil, err
	}
	dialer, err := transport.NewDialer(pc, n.identity, peerVerifier{trust: n.trust}, n.cfg.Transport)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	return dialer, nil
}

// run starts the goroutines of the node.
func (n *Node) run() {
	ctx := n.ctx
	n.spawn(func() { n.acceptLoop(ctx) })
	n.spawn(func() { n.browseLoop(ctx) })
	n.spawn(func() { n.clipRun(ctx) })
	n.spawn(func() { n.historyRun(ctx) })

	n.advertise(ctx)
	for _, peer := range n.pairedPeers(ctx) {
		n.startLink(peer)
	}
}

// spawn runs fn on a goroutine that Stop waits for.
func (n *Node) spawn(fn func()) {
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		fn()
	}()
}

// clipRun keeps the clipboard pipeline running until the node stops.
func (n *Node) clipRun(ctx context.Context) {
	if err := n.clip.Run(ctx); err != nil && ctx.Err() == nil {
		n.log.Error("clipboard pipeline stopped", "error", err)
	}
}

// advertise announces the main listener on the local network.
func (n *Node) advertise(ctx context.Context) {
	if n.listener == nil {
		return
	}
	port := portOf(n.listener.Addr())
	if port == 0 {
		n.log.Warn("main listener has no port to announce")
		return
	}
	if err := n.cfg.Discovery.Advertise(ctx, ports.ServiceMain, port, nil); err != nil {
		n.log.Warn("announcing the device failed", "error", err)
	}
}

// pairedPeers returns the devices the node keeps a link for.
func (n *Node) pairedPeers(ctx context.Context) []trust.Peer {
	peers, err := n.trust.List(ctx, trust.ListFilter{})
	if err != nil {
		n.log.Warn("listing paired devices failed", "error", err)
		return nil
	}
	return peers
}

// startLink returns the link of the peer, creating it when it is missing. The
// link loop of a new link runs until the node stops.
func (n *Node) startLink(peer trust.Peer) *link {
	id := peer.DeviceID()

	n.mu.Lock()
	defer n.mu.Unlock()
	l, ok := n.links[id]
	if !ok {
		l = newLink(id)
		n.links[id] = l
		if n.state == stateRunning && n.ctx != nil {
			ctx := n.ctx
			n.spawn(func() { n.runLink(ctx, l) })
		}
	}
	l.setPeer(peer)
	return l
}

// lookup returns the link of the peer, or nil when the node has none.
func (n *Node) lookup(id model.DeviceID) *link {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.links[id]
}

// linkList returns a snapshot of the links.
func (n *Node) linkList() []*link {
	n.mu.Lock()
	defer n.mu.Unlock()
	links := make([]*link, 0, len(n.links))
	for _, l := range n.links {
		links = append(links, l)
	}
	return links
}

// peerState returns the live state of one device.
func (n *Node) peerState(id model.DeviceID) model.PeerState {
	l := n.lookup(id)
	if l == nil {
		return model.PeerState{DeviceID: id}
	}
	return l.state()
}

// usable reports whether the node is running.
func (n *Node) usable() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state != stateRunning {
		return ErrStopped
	}
	return nil
}

// installClip registers the clipboard handler on a new session.
func (n *Node) installClip(_ context.Context, sess *session.Session) error {
	return sess.Handle(session.KindClipAnnounce, n.clipHandler(sess))
}

// installGossip registers the trust handlers on a new session.
func (n *Node) installGossip(_ context.Context, sess *session.Session) error {
	if n.gossip == nil {
		return errors.New("node: gossip is not ready")
	}
	return n.gossip.Register(sess)
}

// gossipPeersChanged connects to the devices an introduction added.
func (n *Node) gossipPeersChanged(ctx context.Context) {
	for _, peer := range n.pairedPeers(ctx) {
		n.startLink(peer).retry()
	}
	n.probeCandidates(n.ctx)
}

// gossipGroupChanged announces the device again after the discovery key of the
// group changed.
func (n *Node) gossipGroupChanged(context.Context) {
	n.mu.Lock()
	running := n.state == stateRunning && n.ctx != nil
	ctx := n.ctx
	n.mu.Unlock()
	if !running {
		return
	}
	n.advertise(ctx)
}

// stop ends the node once.
func (n *Node) stop(ctx context.Context) error {
	n.stopOnce.Do(func() {
		n.mu.Lock()
		wasNew := n.state == stateNew
		n.state = stateStopped
		cancel := n.cancel
		n.mu.Unlock()
		if wasNew {
			return
		}
		n.stopErr = n.shutdown(ctx, cancel)
	})
	return n.stopErr
}

// shutdown releases the resources of the node.
func (n *Node) shutdown(ctx context.Context, cancel context.CancelFunc) error {
	if cancel != nil {
		cancel()
	}
	if err := n.ClosePairing(); err != nil {
		n.log.Warn("closing the pairing window failed", "error", err)
	}
	if n.gossip != nil {
		n.gossip.Close()
	}
	n.closeSessions()

	if n.listener != nil {
		_ = n.listener.Close()
	}
	if n.dialer != nil {
		_ = n.dialer.Close()
	}
	if err := n.wait(ctx); err != nil {
		return err
	}
	if n.cfg.Discovery != nil {
		if err := n.cfg.Discovery.Stop(); err != nil {
			n.log.Warn("stopping discovery failed", "error", err)
		}
	}
	if n.db != nil {
		return n.db.Close()
	}
	return nil
}

// closeSessions ends the sessions of every link.
func (n *Node) closeSessions() {
	for _, l := range n.linkList() {
		l.close()
	}
}

// wait waits until the goroutines of the node end or ctx expires.
func (n *Node) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// listenUDP binds a real UDP socket for the given port.
func listenUDP(_ context.Context, port int) (net.PacketConn, error) {
	return net.ListenPacket("udp", net.JoinHostPort("", strconv.Itoa(port)))
}
