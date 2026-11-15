package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Application error codes the session sends when it closes a connection.
const (
	// CodeIncompatibleVersion closes a session whose peer announced another
	// protocol version.
	CodeIncompatibleVersion quic.ApplicationErrorCode = 0x10
	// CodeProtocolViolation closes a session whose peer broke the protocol or
	// exceeded a limit.
	CodeProtocolViolation quic.ApplicationErrorCode = 0x11
)

// Errors reported by the session.
var (
	// ErrClosed reports a session that already ended.
	ErrClosed = errors.New("session: closed")
	// ErrNotOpen reports a session that has not completed the Hello exchange.
	ErrNotOpen = errors.New("session: not open")
	// ErrAlreadyOpen reports a second call to Open.
	ErrAlreadyOpen = errors.New("session: already open")
	// ErrIncompatibleVersion reports a peer that speaks another protocol
	// version. The peer is asked to update.
	ErrIncompatibleVersion = errors.New("session: incompatible protocol version")
	// ErrInvalidHello reports a Hello that does not pass validation.
	ErrInvalidHello = errors.New("session: invalid hello")
	// ErrUnknownKind reports a control message or stream kind the session does
	// not know.
	ErrUnknownKind = errors.New("session: unknown kind")
	// ErrInvalidHeader reports a stream header that does not pass validation.
	ErrInvalidHeader = errors.New("session: invalid stream header")
	// ErrTooManyViolations reports a peer that reached the violation threshold.
	ErrTooManyViolations = errors.New("session: too many violations")
	// ErrHandler reports an unusable handler registration.
	ErrHandler = errors.New("session: invalid handler")
)

// Trust is the peer store the session reads peers from. *trust.Store
// implements it.
type Trust interface {
	// Get returns the peer record, including a revoked one.
	Get(ctx context.Context, id model.DeviceID) (trust.Peer, error)
	// Rename sets the display name of a peer.
	Rename(ctx context.Context, id model.DeviceID, name string) error
}

// platformRecorder is the optional part of Trust that records the platform a
// peer announces in Hello.
type platformRecorder interface {
	SetPlatform(ctx context.Context, id model.DeviceID, platform model.Platform) error
}

var _ Trust = (*trust.Store)(nil)

// Handler handles one kind of control message.
type Handler func(ctx context.Context, msg *wirev1.ControlMessage) error

// RequestHandler serves an incoming request stream.
type RequestHandler func(ctx context.Context, req *Request) error

// TransferHandler reads an incoming transfer stream.
type TransferHandler func(ctx context.Context, transfer *Transfer) error

// Config describes a session.
type Config struct {
	// Name is the local device name announced in Hello.
	Name string
	// Platform is the local platform announced in Hello.
	Platform model.Platform
	// Capabilities are the local features announced in Hello.
	Capabilities []string
	// RequireConfirmation is the local clipboard policy announced in Hello.
	RequireConfirmation bool
	// Trust is the peer store. It is required.
	Trust Trust
	// Clock is the time source of the rate limiter. The system clock is used
	// when nil.
	Clock ports.Clock
	// Logger receives session events. Nothing is logged when nil.
	Logger *slog.Logger
}

// Session runs the peer protocol on an authenticated connection: the Hello
// exchange, control messages and streams. Every incoming message is checked
// against the capability matrix of the peer before a handler sees it.
//
// A session is created with New, handlers are registered, then Open exchanges
// Hello and starts serving the connection.
type Session struct {
	conn   *transport.Conn
	peerID model.DeviceID
	cfg    Config
	log    *slog.Logger
	clock  ports.Clock

	control *quic.Stream
	frames  *frameReader

	ctx    context.Context
	cancel context.CancelFunc

	announce *tokenBucket
	events   chan Event
	done     chan struct{}

	writeMu sync.Mutex
	mu      sync.Mutex
	closed  bool
	opened  bool
	err     error
	hello   PeerHello

	handlers  map[Kind]Handler
	requests  map[string]requestRoute
	transfers map[string]transferRoute

	closeOnce    sync.Once
	violations   atomic.Int32
	requestLoad  atomic.Int32
	transferLoad atomic.Int32
}

// requestRoute is the handler of one incoming request payload type.
type requestRoute struct {
	action  trust.Action
	handler RequestHandler
}

// transferRoute is the handler of one incoming transfer payload type.
type transferRoute struct {
	action  trust.Action
	handler TransferHandler
}

// New prepares a session on an authenticated connection. The connection stays
// owned by the caller until it is handed over with Open.
func New(conn *transport.Conn, cfg Config) (*Session, error) {
	switch {
	case conn == nil:
		return nil, errors.New("session: nil connection")
	case cfg.Trust == nil:
		return nil, errors.New("session: nil trust store")
	}
	if err := validateName(cfg.Name); err != nil {
		return nil, err
	}
	if len(cfg.Capabilities) > MaxCapabilities {
		return nil, fmt.Errorf("%w: %d capabilities, limit %d", ErrInvalidHello, len(cfg.Capabilities), MaxCapabilities)
	}
	for _, capability := range cfg.Capabilities {
		if err := validateCapability(capability); err != nil {
			return nil, err
		}
	}

	clock := cfg.Clock
	if clock == nil {
		clock = ports.SystemClock
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	ctx, cancel := context.WithCancel(conn.Context())
	return &Session{
		conn:      conn,
		peerID:    conn.Peer().DeviceID(),
		cfg:       cfg,
		log:       log,
		clock:     clock,
		control:   conn.ControlStream(),
		frames:    newFrameReader(conn.ControlStream()),
		ctx:       ctx,
		cancel:    cancel,
		announce:  newTokenBucket(clock, ClipAnnounceBurst, ClipAnnounceRate),
		events:    make(chan Event, EventBuffer),
		done:      make(chan struct{}),
		handlers:  make(map[Kind]Handler),
		requests:  make(map[string]requestRoute),
		transfers: make(map[string]transferRoute),
	}, nil
}

// Peer returns the authenticated peer of the connection.
func (s *Session) Peer() transport.Peer {
	return s.conn.Peer()
}

// DeviceID returns the device ID of the peer.
func (s *Session) DeviceID() model.DeviceID {
	return s.peerID
}

// Role returns the authenticated trust role of the peer.
func (s *Session) Role() model.Role {
	return s.conn.Role()
}

// PeerHello returns what the peer announced in Hello. It is filled before Open
// returns and holds only the version when the exchange failed.
func (s *Session) PeerHello() PeerHello {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hello
}

// Events returns the session events. The channel is closed when the session ends.
func (s *Session) Events() <-chan Event {
	return s.events
}

// Done is closed when the session ends.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Err returns why the session ended, or nil while it runs and after a clean end.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Violations returns the number of protocol violations the peer committed.
func (s *Session) Violations() int {
	return int(s.violations.Load())
}

// Handle registers fn for a control message kind. One handler per kind, and the
// kinds the session owns itself (Hello and the transport proof) are refused.
func (s *Session) Handle(kind Kind, fn Handler) error {
	switch {
	case kind == KindUnknown || kind == KindHello || kind == KindAuthProof:
		return fmt.Errorf("%w: %s is handled by the session", ErrHandler, kind)
	case fn == nil:
		return fmt.Errorf("%w: nil handler", ErrHandler)
	}
	if _, err := actionOf(kind); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if _, ok := s.handlers[kind]; ok {
		return fmt.Errorf("%w: %s is already handled", ErrHandler, kind)
	}
	s.handlers[kind] = fn
	return nil
}

// Open exchanges Hello with the peer and starts serving the connection.
// Register the handlers before Open: the peer may send messages right after its
// Hello.
func (s *Session) Open(ctx context.Context) error {
	s.mu.Lock()
	switch {
	case s.closed:
		s.mu.Unlock()
		return ErrClosed
	case s.opened:
		s.mu.Unlock()
		return ErrAlreadyOpen
	}
	s.opened = true
	s.mu.Unlock()

	if err := s.handshake(ctx); err != nil {
		if errors.Is(err, ErrIncompatibleVersion) {
			s.emit(Event{Kind: EventPeerNeedsUpdate, Peer: s.peerID, Version: s.PeerHello().Version})
			s.shutdown(err, CodeIncompatibleVersion)
			return err
		}
		s.shutdown(err, CodeProtocolViolation)
		return err
	}

	s.emit(Event{Kind: EventOpened, Peer: s.peerID})
	go s.serveControl()
	go s.acceptRequests()
	go s.acceptTransfers()
	return nil
}

// Send writes one control message to the peer. A message the session sends
// itself, such as Hello, is refused, as is a message over the control message
// limit.
func (s *Session) Send(ctx context.Context, msg *wirev1.ControlMessage) error {
	kind, err := kindOf(msg)
	if err != nil {
		return err
	}
	switch kind {
	case KindHello, KindAuthProof:
		return fmt.Errorf("%w: %s is sent by the session", ErrHandler, kind)
	}

	s.mu.Lock()
	switch {
	case s.closed:
		s.mu.Unlock()
		return ErrClosed
	case !s.opened:
		s.mu.Unlock()
		return ErrNotOpen
	}
	s.mu.Unlock()
	return s.writeControl(ctx, msg)
}

// Close ends the session and closes the connection. It is safe to call more
// than once.
func (s *Session) Close() {
	s.shutdown(nil, 0)
}

// handshake writes the local Hello and validates the peer Hello.
func (s *Session) handshake(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()

	if err := s.writeControl(ctx, s.localHello()); err != nil {
		return fmt.Errorf("session: send hello: %w", err)
	}
	msg, err := s.readControl(ctx)
	if err != nil {
		return fmt.Errorf("session: read hello: %w", err)
	}

	kind, err := kindOf(msg)
	if err != nil {
		return err
	}
	if kind != KindHello {
		return fmt.Errorf("%w: first message is %s", ErrInvalidHello, kind)
	}

	hello, err := parseHello(msg.GetHello())
	s.setHello(hello)
	if err != nil {
		return err
	}
	if hello.Version != ProtocolVersion {
		return fmt.Errorf("%w: peer version %d, want %d", ErrIncompatibleVersion, hello.Version, ProtocolVersion)
	}
	s.recordPeer(ctx, hello)
	return nil
}

// recordPeer stores the display data the peer announced. A store that cannot
// record it does not end the session: the data is informative.
func (s *Session) recordPeer(ctx context.Context, hello PeerHello) {
	if hello.Name != "" {
		if err := s.cfg.Trust.Rename(ctx, s.peerID, hello.Name); err != nil {
			s.log.Warn("session: cannot record the peer name", "peer", s.peerID, "error", err)
		}
	}
	recorder, ok := s.cfg.Trust.(platformRecorder)
	if !ok || hello.Platform == model.PlatformUnknown {
		return
	}
	if err := recorder.SetPlatform(ctx, s.peerID, hello.Platform); err != nil {
		s.log.Warn("session: cannot record the peer platform", "peer", s.peerID, "error", err)
	}
}

// serveControl reads control messages until the session ends.
func (s *Session) serveControl() {
	for {
		msg, err := s.readControl(s.ctx)
		if err != nil {
			s.readFailed(err)
			return
		}
		s.dispatch(msg)
		if s.isClosed() {
			return
		}
	}
}

// dispatch routes one control message to its handler after the capability and
// the rate checks.
func (s *Session) dispatch(msg *wirev1.ControlMessage) {
	kind, err := kindOf(msg)
	if err != nil {
		s.violate("unknown control message")
		return
	}
	if kind == KindHello || kind == KindAuthProof {
		s.violate("unexpected " + kind.String())
		return
	}
	action, err := actionOf(kind)
	if err != nil {
		s.violate("unsupported " + kind.String())
		return
	}
	if !s.authorize(action, kind.String()) {
		return
	}
	if kind == KindClipAnnounce && !s.announce.allow() {
		s.violate("clip announcement rate limit")
		return
	}

	handler, ok := s.controlHandler(kind)
	if !ok {
		s.log.Debug("session: dropped a message without a handler", "peer", s.peerID, "kind", kind.String())
		return
	}
	if err := handler(s.ctx, msg); err != nil {
		s.log.Warn("session: handler failed", "peer", s.peerID, "kind", kind.String(), "error", err)
	}
}

// authorize checks the capability matrix for one incoming message or stream. A
// peer that is no longer in the trust store ends the session.
func (s *Session) authorize(action trust.Action, what string) bool {
	if action == trust.ActionNone {
		return true
	}
	peer, err := s.cfg.Trust.Get(s.ctx, s.peerID)
	if err != nil {
		s.log.Warn("session: peer lookup failed", "peer", s.peerID, "error", err)
		s.shutdown(err, CodeProtocolViolation)
		return false
	}
	if !peer.Allowed(action) {
		s.violate(fmt.Sprintf("%s not allowed for role %s", what, peer.Role))
		return false
	}
	return true
}

// controlHandler returns the handler of the kind.
func (s *Session) controlHandler(kind Kind) (Handler, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	handler, ok := s.handlers[kind]
	return handler, ok
}

// readControl reads one control message, honouring the deadline of ctx.
func (s *Session) readControl(ctx context.Context) (*wirev1.ControlMessage, error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := s.control.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		defer func() { _ = s.control.SetReadDeadline(time.Time{}) }()
	}
	msg := &wirev1.ControlMessage{}
	if err := s.frames.readMessage(MaxControlMessage, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// writeControl writes one control message, honouring the deadline of ctx.
func (s *Session) writeControl(ctx context.Context, msg *wirev1.ControlMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		if err := s.control.SetWriteDeadline(deadline); err != nil {
			return err
		}
		defer func() { _ = s.control.SetWriteDeadline(time.Time{}) }()
	}
	return writeFrame(s.control, msg, MaxControlMessage)
}

// readFailed ends the session after a control stream read error.
func (s *Session) readFailed(err error) {
	if errors.Is(err, ErrFrame) || errors.Is(err, ErrMessage) {
		s.log.Warn("session: malformed control message", "peer", s.peerID, "error", err)
		s.shutdown(err, CodeProtocolViolation)
		return
	}
	s.shutdown(closeReason(err), 0)
}

// violate records a protocol violation of the peer. The offending message is
// dropped and the session ends when the peer reaches MaxViolations.
func (s *Session) violate(reason string) {
	count := s.violations.Add(1)
	s.log.Warn("session: protocol violation", "peer", s.peerID, "reason", reason, "count", count)
	if count >= MaxViolations {
		s.shutdown(fmt.Errorf("%w: %s", ErrTooManyViolations, reason), CodeProtocolViolation)
	}
}

// setHello stores what the peer announced.
func (s *Session) setHello(hello PeerHello) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hello = hello
}

// emit delivers one session event.
func (s *Session) emit(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.sendEventLocked(event)
}

// sendEventLocked queues an event without blocking. A reader that falls behind
// misses events instead of stalling the session.
func (s *Session) sendEventLocked(event Event) {
	select {
	case s.events <- event:
	default:
		s.log.Debug("session: event dropped", "peer", s.peerID, "event", event.Kind.String())
	}
}

// isClosed reports whether the session ended.
func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// shutdown ends the session once, with the given reason and close code.
func (s *Session) shutdown(err error, code quic.ApplicationErrorCode) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		s.sendEventLocked(Event{Kind: EventClosed, Peer: s.peerID, Err: err})
		s.closed = true
		close(s.events)
		s.mu.Unlock()

		_ = s.conn.CloseWithError(code, closeMessage(code))
		s.cancel()
		close(s.done)
	})
}

// closeReason normalizes a read error into the reason a session ended.
func closeReason(err error) error {
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, context.Canceled), peerClosed(err):
		return nil
	default:
		return err
	}
}

// peerClosed reports a connection that the peer ended without a protocol error.
func peerClosed(err error) bool {
	var appErr *quic.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Remote && appErr.ErrorCode == 0
	}
	return false
}

// closeMessage returns the text that accompanies a close code.
func closeMessage(code quic.ApplicationErrorCode) string {
	switch code {
	case CodeIncompatibleVersion:
		return "protocol version not supported"
	case CodeProtocolViolation:
		return "protocol violation"
	default:
		return ""
	}
}
