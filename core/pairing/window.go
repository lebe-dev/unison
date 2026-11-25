package pairing

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/internal/observability"
)

// Window and exchange defaults.
const (
	// DefaultWindow is how long a pairing window stays open.
	DefaultWindow = 3 * time.Minute
	// DefaultRequestTimeout bounds one pairing exchange.
	DefaultRequestTimeout = 2 * time.Minute
	// maxPending bounds the exchanges running at the same time.
	maxPending = 8
	// maxAttempts bounds the pairing connections a window accepts in
	// attemptWindow. Each connection is one try at the short code, so the limit
	// keeps a device from testing codes against the window.
	maxAttempts = 4
	// attemptWindow is the period of the attempt limit.
	attemptWindow = 10 * time.Second
	// eventBuffer is the number of events buffered for the UI.
	eventBuffer = 32
)

// Errors reported by the pairing window.
var (
	// ErrClosed reports a window that is not open.
	ErrClosed = errors.New("pairing: window is closed")
	// ErrRejected reports a pairing that a user refused.
	ErrRejected = errors.New("pairing: pairing rejected")
	// ErrPeerRevoked reports a device on the deny list.
	ErrPeerRevoked = errors.New("pairing: peer is revoked")
	// ErrRoleMismatch reports a device that proposes another role than the one
	// it is paired with.
	ErrRoleMismatch = errors.New("pairing: role differs from the stored peer")
	// ErrPending reports a device that already has an exchange in progress.
	ErrPending = errors.New("pairing: exchange already in progress")
	// ErrBusy reports a window with too many exchanges in progress.
	ErrBusy = errors.New("pairing: too many exchanges in progress")
	// ErrUnknownRequest reports a device without an exchange in progress.
	ErrUnknownRequest = errors.New("pairing: no exchange with this device")
	// ErrDecided reports a device whose request was already decided.
	ErrDecided = errors.New("pairing: request is already decided")
	// ErrInvalidPeer reports inconsistent identity data of a device.
	ErrInvalidPeer = errors.New("pairing: invalid peer")
	// ErrUnsupported reports a request of a flow that this window does not
	// serve.
	ErrUnsupported = errors.New("pairing: unsupported pairing request")
	// ErrUnreachable reports a device that did not answer on any address: it
	// is turned off, or the network keeps the two devices apart.
	ErrUnreachable = errors.New("pairing: device is not reachable over the network")
	// ErrTooManyAttempts reports a window that received more pairing
	// connections than it accepts in one attempt window.
	ErrTooManyAttempts = errors.New("pairing: too many pairing attempts")
)

// ListenPacketFunc binds a packet connection for the pairing listener and for
// outgoing pairing connections.
type ListenPacketFunc func(ctx context.Context) (net.PacketConn, error)

// Discovery announces and browses pairing instances on the local network.
type Discovery interface {
	Advertise(ctx context.Context, service ports.Service, port int, txt map[string]string) error
	Browse(ctx context.Context, service ports.Service) (<-chan ports.Candidate, error)
	StopService(service ports.Service) error
}

// Options configures a pairing window.
type Options struct {
	// Identity is the local device identity.
	Identity *identity.Identity
	// Trust receives the paired device and the group secrets.
	Trust Store
	// Discovery announces the window and finds the candidates of other ones.
	Discovery Discovery
	// Name is the local device name shown to the other device.
	Name string
	// Platform is the local operating system.
	Platform model.Platform
	// Role is the role proposed to the devices this window pairs with.
	Role model.Role
	// Clock is the time source of the window deadline. The system clock is used
	// when nil.
	Clock ports.Clock
	// Logger receives pairing events. Nothing is logged when nil.
	Logger *slog.Logger
	// ListenPacket binds the packet connections. A UDP socket on a random port
	// is bound when nil.
	ListenPacket ListenPacketFunc
	// Window is how long the window stays open. DefaultWindow is used when
	// zero.
	Window time.Duration
	// RequestTimeout bounds one exchange. DefaultRequestTimeout is used when
	// zero.
	RequestTimeout time.Duration
	// Config tunes the QUIC connections of the window.
	Config transport.Config
	// Relay enables pairing discovery and exchanges through the relay when set.
	Relay Relay
	// RelayBeaconInterval is how often the window announces itself on the relay
	// discovery mailbox. One second is used when zero.
	RelayBeaconInterval time.Duration
}

// extendRequest asks the timer to restart with a new duration.
type extendRequest struct {
	after time.Duration
	armed chan struct{}
}

// Window is an open pairing window: a pairing listener, its announcement and
// the exchanges in progress. One window serves any number of devices, each one
// confirmed separately. It is safe for concurrent use.
type Window struct {
	opts  Options
	clock ports.Clock
	log   *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	listener *transport.PairingListener
	events   chan Event
	extend   chan extendRequest
	sink     sink
	relayID  [8]byte

	mu       sync.Mutex
	deadline time.Time
	token    Token
	// tokenBusy reports that a scanning device holds the token until its
	// exchange ends.
	tokenBusy bool
	// attempts holds when the window accepted the recent pairing connections.
	attempts []time.Time
	requests map[model.DeviceID]*exchange
	conns    map[*transport.PairingConn]struct{}
	closed   bool

	relayDiscovery      net.PacketConn
	relaySeen           map[[8]byte]struct{}
	relayInvites        map[[8]byte]*relayInvite
	relayDials          map[[8]byte]struct{}
	relayReady          map[[8]byte]chan struct{}
	relayInviteAttempts []time.Time
	lastRelayBeacon     time.Time

	wg        sync.WaitGroup
	closeOnce sync.Once
	closedCh  chan struct{}
}

// sink receives the events of a window that has no event channel. The scanning
// device uses it, because its exchange is not bound to a window of its own.
type sink func(Event)

// Open opens a pairing window: it binds the pairing listener and announces it on
// the local network. The listener and the announcement live until Close or the
// window timeout.
func Open(ctx context.Context, opts Options) (*Window, error) {
	switch {
	case opts.Identity == nil:
		return nil, errors.New("pairing: nil identity")
	case opts.Trust == nil:
		return nil, errors.New("pairing: nil trust store")
	case opts.Discovery == nil:
		return nil, errors.New("pairing: nil discovery")
	case opts.Name == "":
		return nil, errors.New("pairing: empty device name")
	case opts.Role != model.RolePersonal && opts.Role != model.RoleFriends:
		return nil, fmt.Errorf("pairing: role %s cannot be proposed", opts.Role)
	}
	if opts.Clock == nil {
		opts.Clock = ports.SystemClock
	}
	if opts.ListenPacket == nil {
		opts.ListenPacket = listenUDP
	}
	if opts.Window <= 0 {
		opts.Window = DefaultWindow
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}

	relayID, err := newRelayID()
	if err != nil {
		return nil, err
	}
	w := &Window{
		opts:         opts,
		clock:        opts.Clock,
		log:          opts.Logger,
		events:       make(chan Event, eventBuffer),
		extend:       make(chan extendRequest, 1),
		relayID:      relayID,
		requests:     make(map[model.DeviceID]*exchange),
		conns:        make(map[*transport.PairingConn]struct{}),
		relaySeen:    make(map[[8]byte]struct{}),
		relayInvites: make(map[[8]byte]*relayInvite),
		relayDials:   make(map[[8]byte]struct{}),
		relayReady:   make(map[[8]byte]chan struct{}),
		closedCh:     make(chan struct{}),
	}
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	w.token = token
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.deadline = w.clock.Now().Add(opts.Window)

	pc, err := opts.ListenPacket(w.ctx)
	if err != nil {
		w.cancel()
		return nil, fmt.Errorf("pairing: listen: %w", err)
	}
	listener, err := transport.NewPairingListener(pc, opts.Identity, transport.AcceptPairingPeer, opts.Config)
	if err != nil {
		_ = pc.Close()
		w.cancel()
		return nil, err
	}
	w.listener = listener

	if err := w.announce(); err != nil {
		_ = listener.Close()
		w.cancel()
		return nil, err
	}

	w.wg.Add(3)
	go w.runAccept()
	go w.runBrowse()
	go w.runTimer()
	if opts.Relay != nil {
		w.wg.Add(1)
		go w.runRelayDiscovery()
	}
	return w, nil
}

// Role returns the role the window proposes to the devices it initiates
// pairing with.
func (w *Window) Role() model.Role {
	return w.opts.Role
}

// Addr returns the address of the pairing listener.
func (w *Window) Addr() net.Addr {
	return w.listener.Addr()
}

// Port returns the port of the pairing listener.
func (w *Window) Port() int {
	return portOf(w.listener.Addr())
}

// QR returns the pairing code of the window. A scanning device connects to one
// of the addresses of the code, so addrs are the addresses of the pairing
// listener that the scanning device can reach. When none are given, a listener
// bound to a specific address is used as is, and a listener bound to a wildcard
// address is replaced by the addresses of the local interfaces. The code holds
// a token of the window, so it must not be logged or stored.
func (w *Window) QR(addrs ...string) (QR, error) {
	w.mu.Lock()
	closed, token := w.closed, w.token
	w.mu.Unlock()
	if closed {
		return QR{}, ErrClosed
	}

	addrs, err := w.codeAddrs(addrs)
	if err != nil {
		return QR{}, err
	}
	code := QR{
		DeviceID:    w.opts.Identity.DeviceID(),
		Fingerprint: w.opts.Identity.CertFingerprint(),
		Token:       token,
		Addrs:       addrs,
		Role:        w.opts.Role,
	}
	if _, err := code.URI(); err != nil {
		return QR{}, err
	}
	return code, nil
}

// codeAddrs returns the addresses to put into the pairing code.
func (w *Window) codeAddrs(addrs []string) ([]string, error) {
	if len(addrs) > 0 {
		return addrs, nil
	}
	if addr := w.Addr(); addr != nil {
		if parsed, err := netip.ParseAddrPort(addr.String()); err == nil && !parsed.Addr().IsUnspecified() {
			return []string{parsed.String()}, nil
		}
	}
	return LocalAddresses(w.Port())
}

// claimToken takes the token for one exchange. It reports whether token is the
// current one and no other exchange holds it.
func (w *Window) claimToken(token []byte) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.tokenBusy || !w.token.tokenMatches(token) {
		return false
	}
	w.tokenBusy = true
	return true
}

// releaseToken gives the token back after a failed exchange, or replaces it
// after a completed one.
func (w *Window) releaseToken(used Token, paired bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !paired {
		// Another window opening replaced the token meanwhile; the one of this
		// exchange is gone with it.
		if w.token == used {
			w.tokenBusy = false
		}
		return
	}
	token, err := NewToken()
	if err != nil {
		// Without a fresh token the window stays usable for the SAS flow only.
		w.token = Token{}
		w.tokenBusy = false
		return
	}
	w.token = token
	w.tokenBusy = false
}

// Deadline returns when the window closes unless it is extended.
func (w *Window) Deadline() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.deadline
}

// Events returns the stream of pairing events. The channel is closed when the
// window closes.
func (w *Window) Events() <-chan Event {
	return w.events
}

// Done returns a channel that is closed when the window closes.
func (w *Window) Done() <-chan struct{} {
	return w.closedCh
}

// Extend restarts the auto-close timer.
func (w *Window) Extend() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrClosed
	}
	w.deadline = w.clock.Now().Add(w.opts.Window)
	w.mu.Unlock()

	armed := make(chan struct{})
	select {
	case w.extend <- extendRequest{after: w.opts.Window, armed: armed}:
	case <-w.ctx.Done():
		return ErrClosed
	}
	select {
	case <-armed:
		return nil
	case <-w.ctx.Done():
		return ErrClosed
	}
}

// Confirm accepts the request of the device. The pairing completes when the
// other device accepts as well. Repeating the same decision is a no-op.
func (w *Window) Confirm(ctx context.Context, id model.DeviceID) error {
	return w.decideLocal(ctx, id, true)
}

// Reject refuses the request of the device. Nothing is written to the trust
// store on either device.
func (w *Window) Reject(ctx context.Context, id model.DeviceID) error {
	return w.decideLocal(ctx, id, false)
}

// Dial starts a pairing exchange with the candidate at addr and blocks until
// the devices are paired, the exchange is refused or ctx ends. Events report
// the SAS to show and the outcome.
func (w *Window) Dial(ctx context.Context, addr string) error {
	if addr == "" {
		return errors.New("pairing: empty address")
	}
	if targetID, ok := ParseRelayAddr(addr); ok {
		return w.dialRelay(ctx, targetID)
	}
	target, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("pairing: address %q: %w", addr, err)
	}

	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return ErrClosed
	}

	fl := flow{method: MethodSAS}
	conn, dialer, err := w.connect(ctx, func(dialer *transport.Dialer) (*transport.PairingConn, error) {
		conn, err := dialer.DialPairing(ctx, target, identity.CertFingerprint{})
		if err != nil {
			return nil, unreachableError(err)
		}
		return conn, nil
	})
	if err != nil {
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}
	defer func() { _ = dialer.Close() }()
	defer w.untrack(conn)
	defer func() { _ = conn.Close() }()

	return w.run(ctx, conn, true, fl)
}

// connect opens a dialer and a tracked pairing connection with dial. The
// caller closes the connection and the dialer.
func (w *Window) connect(ctx context.Context, dial func(*transport.Dialer) (*transport.PairingConn, error)) (*transport.PairingConn, *transport.Dialer, error) {
	pc, err := w.opts.ListenPacket(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("pairing: listen: %w", err)
	}
	// The dialer owns the packet connection and closes it.
	dialer, err := transport.NewDialer(pc, w.opts.Identity, unpairedVerifier{}, w.opts.Config)
	if err != nil {
		_ = pc.Close()
		return nil, nil, err
	}

	conn, err := dial(dialer)
	if err != nil {
		_ = dialer.Close()
		return nil, nil, err
	}
	if err := w.track(conn); err != nil {
		_ = conn.Close()
		_ = dialer.Close()
		return nil, nil, err
	}
	return conn, dialer, nil
}

// unreachableError marks a dial failure of a device that did not answer.
func unreachableError(err error) error {
	if transport.IsUnreachable(err) {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	return err
}

// Close stops the listener, withdraws the announcement and drops every exchange
// in progress without writing to the trust store. It blocks until the window is
// fully closed.
func (w *Window) Close() error {
	w.closeOnce.Do(w.teardown)
	<-w.closedCh
	return nil
}

// teardown releases the resources of the window. It is called once.
func (w *Window) teardown() {
	w.mu.Lock()
	w.closed = true
	conns := make([]*transport.PairingConn, 0, len(w.conns))
	for conn := range w.conns {
		conns = append(conns, conn)
	}
	pending := len(w.requests)
	w.mu.Unlock()

	w.cancel()
	if err := w.opts.Discovery.StopService(ports.ServicePairing); err != nil && w.log != nil {
		w.log.Warn("pairing announcement not withdrawn", "error", err)
	}
	// Close the connections before the listener: the listener owns the packet
	// connection, and a connection can only notify its peer while it can still
	// send.
	for _, conn := range conns {
		_ = conn.Close()
	}
	_ = w.listener.Close()
	w.wg.Wait()

	if w.log != nil && pending > 0 {
		w.log.Info("pairing window closed with pending requests", "pending", pending)
	}
	select {
	case w.events <- Event{Kind: EventClosed}:
	default:
	}
	close(w.events)
	close(w.closedCh)
}

// announce publishes the pairing listener on the local network.
func (w *Window) announce() error {
	port := w.Port()
	if port == 0 {
		return errors.New("pairing: listener without a port")
	}
	if err := w.opts.Discovery.Advertise(w.ctx, ports.ServicePairing, port, nil); err != nil {
		return fmt.Errorf("pairing: announce: %w", err)
	}
	return nil
}

// runAccept accepts pairing connections until the window closes. A connection
// that fails the handshake does not stop the window.
func (w *Window) runAccept() {
	defer w.wg.Done()
	defer observability.Recover()
	for {
		conn, err := w.listener.Accept(w.ctx)
		if err != nil {
			if w.isClosed() || w.ctx.Err() != nil {
				return
			}
			if w.log != nil {
				w.log.Warn("pairing connection refused", "error", err)
			}
			continue
		}
		if err := w.trackAccepted(conn); err != nil {
			if w.log != nil && !errors.Is(err, ErrClosed) {
				w.log.Warn("pairing connection dropped", "error", err)
			}
			_ = conn.Close()
			continue
		}
		go func() {
			defer w.untrack(conn)
			defer func() { _ = conn.Close() }()
			defer observability.Recover()
			_ = w.run(w.ctx, conn, false, flow{method: MethodSAS})
		}()
	}
}

// runBrowse reports the pairing candidates found on the local network.
func (w *Window) runBrowse() {
	defer w.wg.Done()
	defer observability.Recover()
	candidates, err := w.opts.Discovery.Browse(w.ctx, ports.ServicePairing)
	if err != nil {
		if w.log != nil && w.ctx.Err() == nil {
			w.log.Warn("pairing browse failed", "error", err)
		}
		return
	}
	for {
		select {
		case <-w.ctx.Done():
			return
		case candidate, ok := <-candidates:
			if !ok {
				return
			}
			w.emit(Event{Kind: EventCandidate, Addr: candidate.Addr, TXT: candidate.TXT})
		}
	}
}

// runTimer closes the window when its time is over or when the context of the
// window ends.
func (w *Window) runTimer() {
	defer w.wg.Done()
	defer observability.Recover()
	timer := w.clock.After(w.opts.Window)
	for {
		select {
		case <-w.ctx.Done():
			if !w.isClosed() {
				go func() {
					defer observability.Recover()
					_ = w.Close()
				}()
			}
			return
		case request := <-w.extend:
			timer = w.clock.After(request.after)
			close(request.armed)
		case <-timer:
			go func() {
				defer observability.Recover()
				_ = w.Close()
			}()
			return
		}
	}
}

// track registers an exchange connection.
func (w *Window) track(conn *transport.PairingConn) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.trackLocked(conn)
}

// trackAccepted registers a connection that a device dialed, after counting the
// attempt. A device that tests the short code needs one connection per try, so
// a window refuses more than maxAttempts of them in attemptWindow.
func (w *Window) trackAccepted(conn *transport.PairingConn) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.recentAttemptsLocked() >= maxAttempts {
		return ErrTooManyAttempts
	}
	w.attempts = append(w.attempts, w.clock.Now())
	return w.trackLocked(conn)
}

// recentAttemptsLocked prunes expired attempt timestamps and returns the count
// within attemptWindow. The caller must hold mu.
func (w *Window) recentAttemptsLocked() int {
	now := w.clock.Now()
	recent := w.attempts[:0]
	for _, at := range w.attempts {
		if now.Sub(at) < attemptWindow {
			recent = append(recent, at)
		}
	}
	w.attempts = recent
	return len(w.attempts)
}

// trackLocked registers a connection with mu held.
func (w *Window) trackLocked(conn *transport.PairingConn) error {
	if w.closed {
		return ErrClosed
	}
	if len(w.conns) >= maxPending {
		return ErrBusy
	}
	w.conns[conn] = struct{}{}
	w.wg.Add(1)
	return nil
}

// untrack unregisters an exchange connection.
func (w *Window) untrack(conn *transport.PairingConn) {
	w.mu.Lock()
	delete(w.conns, conn)
	w.mu.Unlock()
	w.wg.Done()
}

// isClosed reports whether the window is closed.
func (w *Window) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// emit delivers an event to the UI.
func (w *Window) emit(ev Event) {
	if w.sink != nil {
		w.sink(ev)
		return
	}
	select {
	case w.events <- ev:
	case <-w.ctx.Done():
	}
}

// decideLocal records the decision of the local user on the request of a
// device.
func (w *Window) decideLocal(ctx context.Context, id model.DeviceID, accepted bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	ex, ok := w.requests[id]
	w.mu.Unlock()
	if !ok {
		return ErrUnknownRequest
	}
	return ex.decide(accepted)
}

// listenUDP binds a UDP socket on a random port.
func listenUDP(context.Context) (net.PacketConn, error) {
	return net.ListenPacket("udp", ":0")
}

// portOf returns the port of addr, or zero when addr has none.
func portOf(addr net.Addr) int {
	if addr == nil {
		return 0
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return p
}

// unpairedVerifier is the dialer verifier of a pairing connection. The dialer
// never asks it: a pairing peer is authenticated by the SAS flow, not by the
// trust store.
type unpairedVerifier struct{}

// VerifyPeer reports that the verifier is not used.
func (unpairedVerifier) VerifyPeer(context.Context, *x509.Certificate) (transport.Peer, error) {
	return transport.Peer{}, errors.New("pairing: the pairing dialer does not verify peers")
}
