package pairing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/internal/observability"
)

// RelayAddrPrefix is the prefix of a pairing candidate address discovered
// through the relay.
const RelayAddrPrefix = "relay:"

const (
	relayIDLen                 = 8
	relayBeaconLen             = 4 + 1 + relayIDLen
	relaySignalLen             = 4 + 1 + relayIDLen + relayIDLen
	defaultRelayBeaconInterval = time.Second
	relayInviteInterval        = 250 * time.Millisecond
)

const (
	relayMsgBeacon byte = 1
	relayMsgInvite byte = 2
	relayMsgReady  byte = 3
)

var relayMagic = [4]byte{'U', 'P', 'R', '1'}

// Relay opens packet connections to relay mailboxes for pairing discovery and
// pairing exchanges.
type Relay interface {
	OpenDiscovery(ctx context.Context) (net.PacketConn, error)
	OpenSession(ctx context.Context, initiator, responder [8]byte) (net.PacketConn, error)
	PacketSize() int
}

// IsRelayAddr reports whether addr is a valid relay pairing candidate address.
func IsRelayAddr(addr string) bool {
	_, ok := ParseRelayAddr(addr)
	return ok
}

// ParseRelayAddr parses a relay:<16-lowercase-hex> candidate address.
func ParseRelayAddr(addr string) ([8]byte, bool) {
	var id [8]byte
	raw, ok := strings.CutPrefix(addr, RelayAddrPrefix)
	if !ok || len(raw) != relayIDLen*2 {
		return id, false
	}
	for i := range len(raw) {
		c := raw[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return id, false
		}
	}
	if _, err := hex.Decode(id[:], []byte(raw)); err != nil {
		return [8]byte{}, false
	}
	return id, true
}

// FormatRelayAddr formats an 8-byte ephemeral window ID as a relay candidate
// address.
func FormatRelayAddr(id [8]byte) string {
	return RelayAddrPrefix + hex.EncodeToString(id[:])
}

// newRelayID returns a random 8-byte ephemeral window ID.
func newRelayID() ([8]byte, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return id, fmt.Errorf("pairing: generate relay window id: %w", err)
	}
	return id, nil
}

// runRelayDiscovery announces this window on the shared discovery mailbox,
// emits discovered peers as candidates, and handles incoming pairing invites.
func (w *Window) runRelayDiscovery() {
	defer w.wg.Done()
	defer observability.Recover()

	pc, err := w.opts.Relay.OpenDiscovery(w.ctx)
	if err != nil {
		if w.log != nil && w.ctx.Err() == nil {
			w.log.Debug("relay pairing discovery unavailable", "error", err)
		}
		return
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		_ = pc.Close()
		return
	}
	w.relayDiscovery = pc
	w.mu.Unlock()

	stop := context.AfterFunc(w.ctx, func() { _ = pc.Close() })
	defer stop()
	defer func() {
		w.mu.Lock()
		if w.relayDiscovery == pc {
			w.relayDiscovery = nil
		}
		w.mu.Unlock()
		_ = pc.Close()
	}()

	beaconStop := make(chan struct{})
	beaconDone := make(chan struct{})
	go func() {
		defer close(beaconDone)
		defer observability.Recover()
		w.runRelayBeacons(pc, beaconStop)
	}()
	defer func() {
		close(beaconStop)
		<-beaconDone
	}()

	buf := make([]byte, 64)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if w.isClosed() || w.ctx.Err() != nil {
				return
			}
			if w.log != nil {
				w.log.Debug("relay pairing discovery read failed", "error", err)
			}
			return
		}
		w.handleRelayDatagram(pc, buf[:n])
	}
}

// runRelayBeacons sends periodic discovery beacons until stop or window close.
func (w *Window) runRelayBeacons(pc net.PacketConn, stop <-chan struct{}) {
	interval := w.opts.RelayBeaconInterval
	if interval <= 0 {
		interval = defaultRelayBeaconInterval
	}
	w.sendRelayBeacon(pc)
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-stop:
			return
		case <-w.clock.After(interval):
			w.sendRelayBeacon(pc)
		}
	}
}

// handleRelayDatagram dispatches one datagram from the discovery mailbox.
func (w *Window) handleRelayDatagram(pc net.PacketConn, data []byte) {
	if len(data) < 5 || [4]byte(data[:4]) != relayMagic {
		return
	}
	switch data[4] {
	case relayMsgBeacon:
		if len(data) != relayBeaconLen {
			return
		}
		var sender [8]byte
		copy(sender[:], data[5:13])
		if sender == w.relayID {
			return
		}
		w.mu.Lock()
		_, seen := w.relaySeen[sender]
		if !seen {
			w.relaySeen[sender] = struct{}{}
		}
		w.mu.Unlock()
		if !seen {
			w.sendRelayBeacon(pc)
			w.emit(Event{
				Kind: EventCandidate,
				Addr: FormatRelayAddr(sender),
				TXT:  map[string]string{"transport": "relay"},
			})
		}
	case relayMsgInvite:
		if len(data) != relaySignalLen {
			return
		}
		var initiator, responder [8]byte
		copy(initiator[:], data[5:13])
		copy(responder[:], data[13:21])
		if responder != w.relayID || initiator == w.relayID {
			return
		}
		w.handleRelayInvite(pc, initiator)
	case relayMsgReady:
		if len(data) != relaySignalLen {
			return
		}
		var responder, initiator [8]byte
		copy(responder[:], data[5:13])
		copy(initiator[:], data[13:21])
		if initiator != w.relayID || responder == w.relayID {
			return
		}
		w.mu.Lock()
		if ch, ok := w.relayReady[responder]; ok {
			delete(w.relayReady, responder)
			close(ch)
		}
		w.mu.Unlock()
	}
}

// handleRelayInvite deduplicates an incoming relay pairing invite and starts a
// session listener for initiator.
func (w *Window) handleRelayInvite(discoveryPC net.PacketConn, initiator [8]byte) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	if ready, exists := w.relayInvites[initiator]; exists {
		w.mu.Unlock()
		if ready {
			w.sendRelayReady(discoveryPC, initiator)
		}
		return
	}
	if len(w.conns)+len(w.relayInvites) >= maxPending || w.recentAttemptsLocked() >= maxAttempts {
		w.mu.Unlock()
		return
	}
	w.relayInvites[initiator] = false
	w.wg.Add(1)
	w.mu.Unlock()

	go w.acceptRelay(discoveryPC, initiator)
}

// acceptRelay opens the session mailbox for initiator, signals Ready, accepts
// one pairing connection, and runs the SAS exchange.
func (w *Window) acceptRelay(discoveryPC net.PacketConn, initiator [8]byte) {
	defer w.wg.Done()
	defer observability.Recover()
	defer func() {
		w.mu.Lock()
		delete(w.relayInvites, initiator)
		w.mu.Unlock()
	}()

	sessionPC, err := w.opts.Relay.OpenSession(w.ctx, initiator, w.relayID)
	if err != nil {
		if w.log != nil && w.ctx.Err() == nil {
			w.log.Debug("relay pairing session open failed", "error", err)
		}
		return
	}

	pktSize := w.opts.Relay.PacketSize()
	if pktSize < transport.MinPacketSize {
		_ = sessionPC.Close()
		if w.log != nil {
			w.log.Debug("relay packet size too small for pairing", "packet_size", pktSize)
		}
		return
	}

	cfg := w.opts.Config
	cfg.PacketSize = pktSize
	listener, err := transport.NewPairingListener(sessionPC, w.opts.Identity, transport.AcceptPairingPeer, cfg)
	if err != nil {
		_ = sessionPC.Close()
		if w.log != nil && w.ctx.Err() == nil {
			w.log.Debug("relay pairing listener failed", "error", err)
		}
		return
	}
	defer func() { _ = listener.Close() }()

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.relayInvites[initiator] = true
	w.mu.Unlock()

	w.sendRelayReady(discoveryPC, initiator)

	timeout := cfg.HandshakeTimeout
	if timeout <= 0 {
		timeout = transport.HandshakeTimeout
	}
	acceptCtx, cancel := context.WithTimeout(w.ctx, timeout)
	conn, err := listener.Accept(acceptCtx)
	cancel()
	if err != nil {
		if w.log != nil && w.ctx.Err() == nil {
			w.log.Debug("relay pairing accept failed", "error", err)
		}
		return
	}

	if err := w.trackAccepted(conn); err != nil {
		if w.log != nil && !errors.Is(err, ErrClosed) {
			w.log.Warn("relay pairing connection dropped", "error", err)
		}
		_ = conn.Close()
		return
	}
	defer w.untrack(conn)
	defer func() { _ = conn.Close() }()

	_ = w.run(w.ctx, conn, false, flow{method: MethodSAS})
}

// dialRelay opens a session mailbox with targetID, invites the peer on the
// discovery mailbox, and runs the SAS exchange once the peer signals Ready.
func (w *Window) dialRelay(ctx context.Context, targetID [8]byte) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrClosed
	}
	w.wg.Add(1)
	w.mu.Unlock()
	defer w.wg.Done()

	dialCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.ctx, cancel)
	defer stop()
	defer cancel()

	fl := flow{method: MethodSAS}
	if w.opts.Relay == nil {
		err := errors.New("pairing: relay is not configured")
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}
	if targetID == w.relayID {
		err := errors.New("pairing: cannot dial self over relay")
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}

	sessionPC, err := w.opts.Relay.OpenSession(dialCtx, w.relayID, targetID)
	if err != nil {
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}

	pktSize := w.opts.Relay.PacketSize()
	if pktSize < transport.MinPacketSize {
		_ = sessionPC.Close()
		err := fmt.Errorf("pairing: relay packet size %d is below minimum %d", pktSize, transport.MinPacketSize)
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}

	if err := w.waitRelayReady(dialCtx, targetID); err != nil {
		_ = sessionPC.Close()
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}

	cfg := w.opts.Config
	cfg.PacketSize = pktSize
	dialer, err := transport.NewDialer(sessionPC, w.opts.Identity, unpairedVerifier{}, cfg)
	if err != nil {
		_ = sessionPC.Close()
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}

	conn, err := dialer.DialPairing(dialCtx, sessionPC.LocalAddr(), identity.CertFingerprint{})
	if err != nil {
		_ = dialer.Close()
		err = unreachableError(err)
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}
	if err := w.track(conn); err != nil {
		_ = conn.Close()
		_ = dialer.Close()
		w.reportFailure(peerInfo{}, fl, err)
		return err
	}
	defer func() { _ = dialer.Close() }()
	defer w.untrack(conn)
	defer func() { _ = conn.Close() }()

	return w.run(dialCtx, conn, true, fl)
}

// waitRelayReady sends Invite messages until targetID replies with Ready, the
// handshake timeout elapses, or ctx ends.
func (w *Window) waitRelayReady(ctx context.Context, targetID [8]byte) error {
	readyCh := make(chan struct{})
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrClosed
	}
	w.relayReady[targetID] = readyCh
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		if w.relayReady[targetID] == readyCh {
			delete(w.relayReady, targetID)
		}
		w.mu.Unlock()
	}()

	timeout := w.opts.Config.HandshakeTimeout
	if timeout <= 0 {
		timeout = transport.HandshakeTimeout
	}
	deadline := w.clock.After(timeout)

	w.sendRelayInvite(targetID)
	for {
		select {
		case <-readyCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-w.ctx.Done():
			return ErrClosed
		case <-deadline:
			return fmt.Errorf("%w: relay peer did not respond", ErrUnreachable)
		case <-w.clock.After(relayInviteInterval):
			w.sendRelayInvite(targetID)
		}
	}
}

// sendRelayBeacon sends a Beacon datagram on pc.
func (w *Window) sendRelayBeacon(pc net.PacketConn) {
	var msg [relayBeaconLen]byte
	copy(msg[:4], relayMagic[:])
	msg[4] = relayMsgBeacon
	copy(msg[5:13], w.relayID[:])
	_, _ = pc.WriteTo(msg[:], pc.LocalAddr())
}

// sendRelayInvite sends an Invite datagram on the active discovery mailbox.
func (w *Window) sendRelayInvite(responder [8]byte) {
	w.mu.Lock()
	pc := w.relayDiscovery
	w.mu.Unlock()
	if pc == nil {
		return
	}
	var msg [relaySignalLen]byte
	copy(msg[:4], relayMagic[:])
	msg[4] = relayMsgInvite
	copy(msg[5:13], w.relayID[:])
	copy(msg[13:21], responder[:])
	_, _ = pc.WriteTo(msg[:], pc.LocalAddr())
}

// sendRelayReady sends a Ready datagram on pc.
func (w *Window) sendRelayReady(pc net.PacketConn, initiator [8]byte) {
	var msg [relaySignalLen]byte
	copy(msg[:4], relayMagic[:])
	msg[4] = relayMsgReady
	copy(msg[5:13], w.relayID[:])
	copy(msg[13:21], initiator[:])
	_, _ = pc.WriteTo(msg[:], pc.LocalAddr())
}
