package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/internal/observability"
	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// Timeouts of a connection.
const (
	// quicIdleTimeout closes a connection whose peer stopped answering.
	quicIdleTimeout = 60 * time.Second
	// quicKeepAlive is the interval of keepalive packets on an idle connection.
	quicKeepAlive = 15 * time.Second
	// quicHandshakeTimeout bounds the QUIC handshake.
	quicHandshakeTimeout = 10 * time.Second
	// httpHeaderTimeout bounds the request header of the HTTP endpoints.
	httpHeaderTimeout = 5 * time.Second
)

// Addrs are the listening addresses of a server.
type Addrs struct {
	// QUIC is the UDP address of the QUIC transport.
	QUIC string
	// WS is the TCP address of the WebSocket fallback.
	WS string
	// HTTP is the TCP address of health and metrics.
	HTTP string
}

// Server relays datagrams between the members of a mailbox.
type Server struct {
	cfg     Config
	log     *slog.Logger
	metrics *metrics
	tokens  *tokenSet
	boxes   *registry

	udpConn       *net.UDPConn
	quicTransport *quic.Transport
	quicListener  *quic.Listener

	wsListener net.Listener
	wsServer   *http.Server

	httpListener net.Listener
	httpServer   *http.Server

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	peers    map[*peer]struct{}
	nextConn uint64
	closed   bool
}

// New returns a server for a valid configuration. It binds no socket; Start
// does.
func New(cfg Config) (*Server, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	logger := cfg.Logger
	if logger == nil {
		level, err := logLevel(cfg.LogLevel)
		if err != nil {
			return nil, err
		}
		logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := newMetrics(cfg.Version)
	return &Server{
		cfg:     cfg,
		log:     logger,
		metrics: m,
		boxes:   newRegistry(cfg.MaxMailboxesTotal, m, logger),
		ctx:     ctx,
		cancel:  cancel,
		peers:   make(map[*peer]struct{}),
	}, nil
}

// Start loads the TLS certificate and the tokens and starts serving.
func (s *Server) Start() error {
	cert, err := tls.LoadX509KeyPair(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("relay: load TLS certificate: %w", err)
	}
	tokens, err := s.cfg.credentials()
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return errors.New("relay: no access tokens configured")
	}
	s.tokens = newTokenSet(tokens, s.cfg, time.Now())

	if err := s.listenQUIC(cert); err != nil {
		_ = s.Close()
		return err
	}
	if err := s.listenWS(cert); err != nil {
		_ = s.Close()
		return err
	}
	if err := s.listenHTTP(); err != nil {
		_ = s.Close()
		return err
	}

	addrs := s.Addrs()
	s.log.Info("relay started",
		"version", s.cfg.Version,
		"quic", addrs.QUIC,
		"ws", addrs.WS,
		"http", addrs.HTTP,
		"tokens", len(tokens),
	)
	return nil
}

// Addrs reports the listening addresses. The fields are empty before Start.
func (s *Server) Addrs() Addrs {
	var addrs Addrs
	if s.quicListener != nil {
		addrs.QUIC = s.quicListener.Addr().String()
	}
	if s.wsListener != nil {
		addrs.WS = s.wsListener.Addr().String()
	}
	if s.httpListener != nil {
		addrs.HTTP = s.httpListener.Addr().String()
	}
	return addrs
}

// Close stops the listeners, tells the clients that the relay shuts down and
// waits for the connections to end.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	peers := make([]*peer, 0, len(s.peers))
	for p := range s.peers {
		peers = append(peers, p)
	}
	s.mu.Unlock()

	s.cancel()
	for _, p := range peers {
		p.stop(relayv1.RelayStatus_RELAY_STATUS_SHUTTING_DOWN)
	}

	var err error
	err = errors.Join(err, closeAll(s.quicListener, s.quicTransport, s.udpConn))
	err = errors.Join(err, closeAll(s.wsServer, s.wsListener))
	err = errors.Join(err, closeAll(s.httpServer, s.httpListener))

	s.wg.Wait()
	s.log.Info("relay stopped")
	return err
}

// closeAll closes the closers and drops the error of an already closed one.
func closeAll(closers ...io.Closer) error {
	var err error
	for _, closer := range closers {
		if closer == nil {
			continue
		}
		if cerr := closer.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			err = errors.Join(err, cerr)
		}
	}
	return err
}

// capacity reports whether another connection fits.
func (s *Server) capacity() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.peers) < s.cfg.MaxConnections
}

// newPeer registers a connection with the server.
func (s *Server) newPeer(transport int, w connWriter) *peer {
	p := &peer{
		srv:       s,
		transport: transport,
		writer:    w,
		out:       make(chan []byte, outQueue),
		done:      make(chan struct{}),
		started:   time.Now(),
	}
	s.mu.Lock()
	s.nextConn++
	p.id = s.nextConn
	s.peers[p] = struct{}{}
	s.mu.Unlock()
	s.metrics.connect(transport)
	return p
}

// removePeer forgets a connection.
func (s *Server) removePeer(p *peer) {
	s.mu.Lock()
	delete(s.peers, p)
	s.mu.Unlock()
	s.metrics.disconnect(p.transport)
}

// limits reports the limits that a connection has to respect.
func (s *Server) limits() *relayv1.Limits {
	return &relayv1.Limits{
		MaxMailboxes:       uint32(s.cfg.MaxMailboxes),
		MaxDatagramBytes:   uint32(s.cfg.MaxDatagramBytes),
		RatePerSecond:      uint32(s.cfg.RatePerSecond),
		RateBurst:          uint32(s.cfg.RateBurst),
		QuotaBytes:         uint64(s.cfg.QuotaBytes),
		QuotaWindowSeconds: uint64(s.cfg.QuotaWindow / time.Second),
		MailboxUpdates:     !s.cfg.NoMailboxUpdates,
	}
}

// register validates a registration, joins its mailboxes and counts the
// outcome.
func (s *Server) register(p *peer, reg *relayv1.Register) relayv1.RelayStatus {
	status := s.authenticate(p, reg)
	s.metrics.registration(status)
	mailboxes := len(reg.GetMailboxIds())
	if status == relayv1.RelayStatus_RELAY_STATUS_OK {
		s.log.Debug("connection registered",
			"conn", p.id,
			"transport", transportLabels[p.transport],
			"mailboxes", mailboxes,
		)
		return status
	}
	s.log.Debug("registration rejected",
		"conn", p.id,
		"transport", transportLabels[p.transport],
		"status", statusLabel(status),
		"mailboxes", mailboxes,
	)
	return status
}

// authenticate checks the token and the mailboxes of a registration.
func (s *Server) authenticate(p *peer, reg *relayv1.Register) relayv1.RelayStatus {
	token := reg.GetToken()
	if len(token) == 0 || len(token) > maxTokenBytes {
		return relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED
	}
	state := s.tokens.find(token)
	if state == nil {
		return relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED
	}

	raw := reg.GetMailboxIds()
	if len(raw) == 0 || len(raw) > s.cfg.MaxMailboxes {
		return relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT
	}
	ids := make([]mailboxID, 0, len(raw))
	for _, id := range raw {
		mailbox, ok := mailboxIDOf(id)
		if !ok {
			return relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT
		}
		ids = append(ids, mailbox)
	}
	if err := s.boxes.join(p, ids); err != nil {
		if errors.Is(err, errRegistryFull) {
			return relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE
		}
		return relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT
	}
	p.token = state
	return relayv1.RelayStatus_RELAY_STATUS_OK
}

// forward relays one datagram to the other members of its mailbox. A datagram
// keeps its place in the connection only when it passes the checks; every
// rejection is counted and none of them ends the connection, except for a token
// that ran out of quota.
func (s *Server) forward(p *peer, data []byte) {
	if p.stopped() {
		return
	}
	if len(data) > s.cfg.MaxDatagramBytes+frameOverhead {
		s.metrics.dropped(dropTooLarge)
		return
	}
	frame, err := parseFrame(data, s.cfg.MaxDatagramBytes+frameOverhead)
	if err != nil {
		s.metrics.dropped(dropMalformed)
		return
	}
	if update := frame.GetUpdate(); update != nil {
		s.update(p, len(data), update)
		return
	}
	datagram := frame.GetDatagram()
	if datagram == nil {
		s.metrics.dropped(dropMalformed)
		return
	}
	id, ok := mailboxIDOf(datagram.GetMailboxId())
	if !ok {
		s.metrics.dropped(dropMalformed)
		return
	}
	payload := datagram.GetPayload()
	switch {
	case len(payload) == 0:
		s.metrics.dropped(dropMalformed)
		return
	case len(payload) > s.cfg.MaxDatagramBytes:
		s.metrics.dropped(dropTooLarge)
		return
	}
	if !s.boxes.member(p, id) {
		s.metrics.dropped(dropNotMember)
		return
	}

	switch err := p.token.take(time.Now(), len(payload)); {
	case errors.Is(err, errQuotaExceeded):
		s.log.Warn("token quota exceeded",
			"conn", p.id,
			"transport", transportLabels[p.transport],
		)
		p.stop(relayv1.RelayStatus_RELAY_STATUS_QUOTA_EXCEEDED)
		return
	case err != nil:
		s.metrics.dropped(dropRateLimit)
		return
	}

	targets := s.boxes.targets(id, p)
	s.metrics.received(p.transport, len(data))
	if len(targets) == 0 {
		s.metrics.dropped(dropNoPeer)
		return
	}
	for _, target := range targets {
		target.send(data)
	}
}

// update replaces the mailbox set of a registered connection and answers with
// the outcome. Updates count against the rate and the quota of the token like
// datagrams, so they cannot be used to flood the relay.
func (s *Server) update(p *peer, size int, update *relayv1.Update) {
	if s.cfg.NoMailboxUpdates {
		s.metrics.dropped(dropMalformed)
		return
	}
	switch err := p.token.take(time.Now(), size); {
	case errors.Is(err, errQuotaExceeded):
		s.log.Warn("token quota exceeded",
			"conn", p.id,
			"transport", transportLabels[p.transport],
		)
		p.stop(relayv1.RelayStatus_RELAY_STATUS_QUOTA_EXCEEDED)
		return
	case err != nil:
		s.metrics.dropped(dropRateLimit)
		return
	}

	seq := update.GetSeq()
	switch {
	case seq != 0 && seq < p.updateSeq:
		// A late copy of an update the connection already replaced.
		return
	case seq != 0 && seq == p.updateSeq:
		// The client did not get the answer and sent the update again.
		p.answer(seq, p.updateStatus)
		return
	}

	status := s.applyUpdate(p, update.GetMailboxIds())
	if seq != 0 {
		p.updateSeq, p.updateStatus = seq, status
	}
	s.metrics.update(status)
	s.log.Debug("mailboxes updated",
		"conn", p.id,
		"transport", transportLabels[p.transport],
		"status", statusLabel(status),
		"mailboxes", len(update.GetMailboxIds()),
	)
	p.answer(seq, status)
}

// applyUpdate validates the mailboxes of an update and makes them the set of
// the connection.
func (s *Server) applyUpdate(p *peer, raw [][]byte) relayv1.RelayStatus {
	if len(raw) == 0 || len(raw) > s.cfg.MaxMailboxes {
		return relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT
	}
	ids := make([]mailboxID, 0, len(raw))
	for _, id := range raw {
		mailbox, ok := mailboxIDOf(id)
		if !ok {
			return relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT
		}
		ids = append(ids, mailbox)
	}
	switch err := s.boxes.replace(p, ids); {
	case err == nil:
		return relayv1.RelayStatus_RELAY_STATUS_OK
	case errors.Is(err, errRegistryFull):
		return relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE
	default:
		return relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT
	}
}

// connWriter is the transport side of a connection.
type connWriter interface {
	// write sends one frame to the client.
	write(frame []byte) error
	// close ends the connection with a status.
	close(status relayv1.RelayStatus) error
}

// peer is one client connection. It joins mailboxes after a successful
// registration and leaves them when the connection ends.
type peer struct {
	srv       *Server
	id        uint64
	transport int
	writer    connWriter
	token     *tokenState
	started   time.Time
	// updateSeq and updateStatus are the last applied update and its
	// outcome. Only the receive loop of the connection touches them.
	updateSeq    uint64
	updateStatus relayv1.RelayStatus

	out  chan []byte
	done chan struct{}
	once sync.Once
}

// answer queues the answer to a mailbox update.
func (p *peer) answer(seq uint64, status relayv1.RelayStatus) {
	if frame := updateResultFrame(seq, status); frame != nil {
		p.send(frame)
	}
}

// send queues a frame for the client. A client that cannot keep up loses
// datagrams, which an unreliable transport does anyway.
func (p *peer) send(frame []byte) {
	select {
	case <-p.done:
	case p.out <- frame:
	default:
		p.srv.metrics.dropped(dropBackpressure)
	}
}

// writeLoop writes the queued frames to the client.
func (p *peer) writeLoop() {
	defer observability.Recover()

	for {
		select {
		case <-p.done:
			return
		case frame := <-p.out:
			if err := p.writer.write(frame); err != nil {
				if errors.Is(err, errFrameTooLarge) {
					p.srv.metrics.dropped(dropTooLarge)
					continue
				}
				p.srv.metrics.dropped(dropWrite)
				p.srv.log.Debug("write to client failed",
					"conn", p.id,
					"transport", transportLabels[p.transport],
					"error", err,
				)
				p.stop(relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED)
				return
			}
			p.srv.metrics.sent(p.transport, len(frame))
		}
	}
}

// stop ends the connection once. A status other than unspecified tells the
// client why the relay closes it.
func (p *peer) stop(status relayv1.RelayStatus) {
	p.once.Do(func() {
		if status != relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED {
			if frame := closeFrame(status); frame != nil {
				_ = p.writer.write(frame)
			}
		}
		p.srv.boxes.leave(p)
		p.srv.removePeer(p)
		close(p.done)
		_ = p.writer.close(status)
		p.srv.log.Debug("connection closed",
			"conn", p.id,
			"transport", transportLabels[p.transport],
			"status", statusLabel(status),
			"duration", time.Since(p.started).Round(time.Millisecond),
		)
	})
}

// stopped reports whether the connection already ended.
func (p *peer) stopped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}
