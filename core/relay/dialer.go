package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// maxKeyBytes bounds the rendezvous key of a pair.
const maxKeyBytes = 64

// errReconnect reports a connection that the client replaced on purpose.
var errReconnect = errors.New("relay: reconnecting")

// errDropped reports a datagram that a congested transport dropped.
var errDropped = errors.New("relay: datagram dropped")

// errRollover reports a connection that ended at a day boundary.
var errRollover = errors.New("relay: day rollover")

// carrier is one connection to the relay: the QUIC datagram transport or the
// WebSocket fallback.
type carrier interface {
	// register sends the registration and returns the limits of the relay.
	register(ctx context.Context, token string, ids []ID) (Limits, error)
	// send transmits one frame without waiting for the network.
	send(frame []byte) error
	// receive hands every received frame to fn until the connection ends.
	receive(ctx context.Context, fn func([]byte)) error
	// transport reports the kind of the carrier.
	transport() Transport
	// close ends the connection.
	close() error
}

// Dialer keeps one connection to the relay and the mailboxes of the local
// device. It reconnects with backoff and registers the mailboxes again.
type Dialer struct {
	cfg   Config
	log   *slog.Logger
	clock ports.Clock
	eps   endpoints

	mu       sync.Mutex
	conns    map[string]*Conn
	nextName int
	status   Status
	sender   carrier
	byID     map[ID]*Conn
	rev      uint64
	// registered is the revision of the mailbox set the connection carries.
	registered uint64
	// lastErr is the failure of the last connection attempt.
	lastErr    error
	restart    bool
	rollover   bool
	preferWS   bool
	running    bool
	closed     bool
	cancelConn context.CancelFunc
	// wait is closed and replaced whenever the state changes.
	wait chan struct{}
	// kick requests a connection attempt.
	kick chan struct{}
}

// NewDialer validates the configuration and returns a client that is not
// connected yet.
func NewDialer(cfg Config) (*Dialer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()
	eps, err := cfg.endpoints()
	if err != nil {
		return nil, err
	}

	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = ports.SystemClock
	}
	return &Dialer{
		cfg:   cfg,
		log:   log,
		clock: clock,
		eps:   eps,
		conns: make(map[string]*Conn),
		wait:  make(chan struct{}),
		kick:  make(chan struct{}, 1),
	}, nil
}

// Run connects to the relay and keeps the connection until ctx ends. It
// reconnects with backoff after a failure and registers the mailboxes of the
// pairs that are open at the time. One call runs the client.
func (d *Dialer) Run(ctx context.Context) error {
	d.mu.Lock()
	if d.running || d.closed {
		d.mu.Unlock()
		return errors.New("relay: client is already running")
	}
	d.running = true
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
	}()

	delay := d.cfg.ReconnectDelay
	for {
		if ctx.Err() != nil {
			return nil
		}
		ids, rev, byID := d.mailboxSet()
		if len(ids) == 0 {
			// Nothing to register: wait for a pair instead of retrying.
			if !d.waitFor(ctx, 0) {
				return nil
			}
			continue
		}

		registered, err := d.connect(ctx, ids, byID, rev)
		if ctx.Err() != nil {
			return nil
		}
		if registered {
			delay = d.cfg.ReconnectDelay
		}
		if errors.Is(err, errReconnect) || errors.Is(err, errRollover) {
			continue
		}
		if err != nil {
			d.log.Debug("relay connection ended", "error", err)
		}
		if !d.waitFor(ctx, delay) {
			return nil
		}
		delay = min(delay*2, d.cfg.ReconnectLimit)
	}
}

// Open returns a packet connection for the pair with the given rendezvous key.
// It waits until the relay carries the mailbox of the pair. Close the returned
// connection to release the mailbox.
func (d *Dialer) Open(ctx context.Context, key []byte) (*Conn, error) {
	switch {
	case len(key) == 0:
		return nil, errors.New("relay: empty rendezvous key")
	case len(key) > maxKeyBytes:
		return nil, errors.New("relay: rendezvous key is too long")
	}

	conn, rev, err := d.add(key)
	if err != nil {
		return nil, err
	}
	limits, err := d.waitReady(ctx, rev)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if limits.PacketSize() < MinPacketSize {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %d bytes, at least %d are needed",
			ErrDatagramTooSmall, limits.MaxDatagramBytes, MinPacketSize+datagramOverhead(MinPacketSize))
	}
	return conn, nil
}

// Status returns the state of the relay connection.
func (d *Dialer) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

// PacketSize returns the largest QUIC packet that fits into one datagram of the
// connected relay, or zero when it is not connected.
func (d *Dialer) PacketSize() int {
	return d.Status().Limits.PacketSize()
}

// Changes returns a channel that is closed when the state of the connection
// changes. Call it again after every change.
func (d *Dialer) Changes() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.wait
}

// NetworkChanged tells the client that the network changed, so a connection
// that fell back to WebSocket tries QUIC again.
func (d *Dialer) NetworkChanged() {
	d.mu.Lock()
	d.preferWS = false
	d.mu.Unlock()
	d.wake()
}

// Close stops the client and every mailbox.
func (d *Dialer) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	cancel := d.cancelConn
	conns := make([]*Conn, 0, len(d.conns))
	for _, c := range d.conns {
		conns = append(conns, c)
	}
	d.notifyLocked()
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, c := range conns {
		c.close()
	}
	d.wake()
	return nil
}

// add reserves a packet connection for a pair and returns its revision. Opening
// a pair twice returns the existing connection.
func (d *Dialer) add(key []byte) (*Conn, uint64, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, 0, ErrClosed
	}
	if existing, ok := d.conns[string(key)]; ok {
		rev := existing.pendingRev
		d.mu.Unlock()
		return existing, rev, nil
	}
	d.nextName++
	conn := newConn(d, slices.Clone(key), Addr{name: fmt.Sprintf("relay:%d", d.nextName)}, d.cfg.Queue)
	d.conns[string(key)] = conn
	d.rev++
	rev := d.rev
	conn.pendingRev = rev
	live := d.sender != nil
	d.mu.Unlock()

	if live {
		d.requestReconnect()
	} else {
		d.wake()
	}
	return conn, rev, nil
}

// remove releases the mailbox of a pair.
func (d *Dialer) remove(c *Conn) {
	d.mu.Lock()
	if current, ok := d.conns[c.keyString()]; ok && current == c {
		delete(d.conns, c.keyString())
	}
	d.rev++
	if d.closed {
		d.mu.Unlock()
		return
	}
	live := d.sender != nil
	d.notifyLocked()
	d.mu.Unlock()

	if live {
		d.requestReconnect()
	} else {
		d.wake()
	}
}

// waitReady waits until the relay carries the mailboxes of revision rev.
func (d *Dialer) waitReady(ctx context.Context, rev uint64) (Limits, error) {
	for {
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			return Limits{}, ErrClosed
		}
		if d.status.Connected && d.registered >= rev {
			limits := d.status.Limits
			d.mu.Unlock()
			return limits, nil
		}
		if errors.Is(d.lastErr, ErrRejected) {
			err := d.lastErr
			d.mu.Unlock()
			return Limits{}, err
		}
		wait := d.wait
		d.mu.Unlock()

		select {
		case <-ctx.Done():
			return Limits{}, ctx.Err()
		case <-wait:
		}
	}
}

// requestRollover drops the live connection at a day boundary, so the client
// registers the mailboxes of the next day.
func (d *Dialer) requestRollover() {
	d.mu.Lock()
	d.rollover = true
	cancel := d.cancelConn
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// requestReconnect drops the live connection so the client registers the
// current mailbox set again.
func (d *Dialer) requestReconnect() {
	d.mu.Lock()
	d.restart = true
	cancel := d.cancelConn
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	d.wake()
}

// mailboxSet returns the mailboxes to register and the revision of the set.
func (d *Dialer) mailboxSet() ([]ID, uint64, map[ID]*Conn) {
	d.mu.Lock()
	defer d.mu.Unlock()

	type entry struct {
		id   ID
		conn *Conn
	}
	entries := make([]entry, 0, 2*len(d.conns))
	seen := make(map[ID]struct{}, 2*len(d.conns))
	now := d.clock.Now()
	for _, c := range d.conns {
		for _, id := range c.mailboxes(now, d.cfg.Rollover) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			entries = append(entries, entry{id: id, conn: c})
		}
	}
	if len(entries) > maxMailboxes {
		d.log.Warn("too many relay mailboxes for one connection", "mailboxes", len(entries))
		entries = entries[:maxMailboxes]
	}

	ids := make([]ID, 0, len(entries))
	byID := make(map[ID]*Conn, len(entries))
	for _, e := range entries {
		ids = append(ids, e.id)
		byID[e.id] = e.conn
	}
	return ids, d.rev, byID
}

// connect dials and registers one connection, then serves it until it ends.
func (d *Dialer) connect(ctx context.Context, ids []ID, byID map[ID]*Conn, rev uint64) (bool, error) {
	conn, err := d.dial(ctx)
	if err != nil {
		return false, err
	}
	limits, err := conn.register(ctx, d.cfg.Token, ids)
	if err != nil {
		_ = conn.close()
		d.fail(err)
		return false, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The connection ends at the next UTC midnight, when the mailboxes of the
	// day change.
	until := nextMidnight(d.clock.Now()).Sub(d.clock.Now())
	go func() {
		select {
		case <-runCtx.Done():
		case <-d.clock.After(until):
			d.requestRollover()
		}
	}()

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		_ = conn.close()
		return false, ErrClosed
	}
	d.sender = conn
	d.byID = byID
	d.registered = rev
	d.lastErr = nil
	d.status = Status{Connected: true, Transport: conn.transport(), Limits: limits}
	d.cancelConn = cancel
	stale := d.rev != rev
	d.notifyLocked()
	d.mu.Unlock()

	if stale {
		// The mailboxes changed while the connection was opening, so the next
		// connection registers the current set.
		d.requestReconnect()
	}

	d.log.Debug("relay connected",
		"transport", conn.transport(),
		"mailboxes", len(ids),
		"max_datagram_bytes", limits.MaxDatagramBytes,
		"packet_size", limits.PacketSize(),
	)

	err = conn.receive(runCtx, d.dispatch)

	d.mu.Lock()
	d.sender = nil
	d.byID = nil
	d.status = Status{}
	d.cancelConn = nil
	if err != nil && !errors.Is(err, context.Canceled) {
		d.lastErr = err
	}
	restart := d.restart
	rollover := d.rollover
	d.restart = false
	d.rollover = false
	d.notifyLocked()
	d.mu.Unlock()

	_ = conn.close()

	switch {
	case restart || errors.Is(err, context.Canceled) && !rollover:
		return true, errReconnect
	case rollover:
		return true, errRollover
	default:
		return true, err
	}
}

// fail records the failure of a connection attempt and wakes the callers that
// wait for the relay.
func (d *Dialer) fail(err error) {
	d.mu.Lock()
	if !errors.Is(err, context.Canceled) {
		d.lastErr = err
	}
	d.notifyLocked()
	d.mu.Unlock()
}

// dispatch routes one received frame to its mailbox.
func (d *Dialer) dispatch(data []byte) {
	frame, err := parseFrame(data, maxRelayDatagrams+frameOverheadBytes)
	if err != nil {
		d.log.Debug("relay frame dropped", "error", err)
		return
	}
	datagram := frame.GetDatagram()
	if datagram == nil {
		return
	}
	id, ok := mailboxIDOf(datagram.GetMailboxId())
	if !ok {
		return
	}

	d.mu.Lock()
	conn := d.byID[id]
	limit := d.status.Limits.MaxDatagramBytes
	d.mu.Unlock()

	payload := datagram.GetPayload()
	if conn == nil || len(payload) == 0 || len(payload) > limit {
		return
	}
	conn.push(payload)
}

// send transmits one payload of a packet connection.
func (d *Dialer) send(c *Conn, payload []byte) error {
	if c.isClosed() {
		return net.ErrClosed
	}
	if c.writeExpired() {
		return fmt.Errorf("relay: %w", os.ErrDeadlineExceeded)
	}
	if len(payload) == 0 {
		return nil
	}

	d.mu.Lock()
	sender := d.sender
	limits := d.status.Limits
	d.mu.Unlock()

	if sender == nil {
		// No connection: the datagram is lost, the peer retransmits.
		return nil
	}
	if size := len(payload) + datagramOverhead(len(payload)); size > limits.MaxDatagramBytes {
		return fmt.Errorf("%w: %d bytes, the relay allows %d", ErrDatagramTooLarge, size, limits.MaxDatagramBytes)
	}
	frame, err := datagramFrame(MailboxID(c.key, d.clock.Now()), payload)
	if err != nil {
		return err
	}
	if err := sender.send(frame); err != nil && !errors.Is(err, errDropped) {
		d.log.Debug("relay send failed", "error", err)
	}
	return nil
}

// dial connects to the relay: QUIC first and the WebSocket fallback after.
func (d *Dialer) dial(ctx context.Context) (carrier, error) {
	if !d.websocketOnly() {
		dialCtx, cancel := context.WithTimeout(ctx, d.cfg.DialTimeout)
		conn, err := d.dialQUIC(dialCtx)
		cancel()
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		d.log.Debug("relay QUIC attempt failed", "error", err)
		d.setWebsocketOnly()
	}

	dialCtx, cancel := context.WithTimeout(ctx, d.cfg.DialTimeout)
	defer cancel()
	return d.dialWebSocket(dialCtx)
}

// websocketOnly reports whether the last attempt had to fall back to WebSocket.
func (d *Dialer) websocketOnly() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.preferWS
}

// setWebsocketOnly remembers that UDP did not get through.
func (d *Dialer) setWebsocketOnly() {
	d.mu.Lock()
	d.preferWS = true
	d.mu.Unlock()
}

// notifyLocked wakes the callers that wait for a state change. The caller must
// hold d.mu.
func (d *Dialer) notifyLocked() {
	close(d.wait)
	d.wait = make(chan struct{})
}

// wake requests a connection attempt.
func (d *Dialer) wake() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// waitFor waits for the delay, a wake-up or the end of the context. A zero delay
// waits for the wake-up only.
func (d *Dialer) waitFor(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-d.kick:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-d.kick:
		return true
	case <-timer.C:
		return true
	}
}
