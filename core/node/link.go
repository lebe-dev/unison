package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/discovery"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/trust"
)

// backoffFactor multiplies the reconnect delay after a failed attempt.
const backoffFactor = 2

// acceptRetryDelay keeps a failing accept loop from spinning.
const acceptRetryDelay = 50 * time.Millisecond

// errSuperseded reports a connection that a concurrent connection replaced.
var errSuperseded = errors.New("node: connection superseded")

// taggedDiscovery is a discovery that reports whether a candidate carries the
// group tag. A platform discovery that decides membership itself, or a test
// bus, does not implement it and its candidates are dialed as they are.
type taggedDiscovery interface {
	BrowseCandidates(ctx context.Context, service ports.Service) (<-chan discovery.Candidate, error)
}

// link is the connection state of one paired device.
type link struct {
	id model.DeviceID

	mu       sync.Mutex
	peer     trust.Peer
	conn     *transport.Conn
	sess     *session.Session
	online   bool
	upgrade  bool
	stopped  bool
	gen      uint64
	dialing  bool
	attempts int
	blocked  bool
	kick     chan struct{}
}

// newLink creates an offline link.
func newLink(id model.DeviceID) *link {
	return &link{id: id, kick: make(chan struct{}, 1)}
}

// setPeer applies the peer record.
func (l *link) setPeer(peer trust.Peer) {
	l.mu.Lock()
	l.peer = peer
	l.mu.Unlock()
}

// state returns the live state of the link.
func (l *link) state() model.PeerState {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := model.PeerState{DeviceID: l.id}
	if l.online {
		state.Online = true
		state.Path = model.PathLAN
	}
	return state
}

// status returns whether the link is online and whether the peer cannot be
// reached at all, which is the case for another protocol version.
func (l *link) status() (online, blocked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.online, l.blocked
}

// clipTarget returns the session to announce clipboard items on and the peer
// record, or ok false when the peer may not receive them.
func (l *link) clipTarget() (sess *session.Session, peer trust.Peer, ok bool) {
	sess, peer, ok = l.onlineSession()
	if !ok || !peer.Allowed(trust.ActionReceiveClipAnnounce) {
		return nil, trust.Peer{}, false
	}
	return sess, peer, true
}

// onlineSession returns the live session of the link and its peer record.
func (l *link) onlineSession() (sess *session.Session, peer trust.Peer, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.online || l.sess == nil {
		return nil, trust.Peer{}, false
	}
	return l.sess, l.peer, true
}

// close ends the session or the connection of the link.
func (l *link) close() {
	l.mu.Lock()
	sess := l.sess
	conn := l.conn
	l.mu.Unlock()

	if sess != nil {
		sess.Close()
		return
	}
	if conn != nil {
		_ = conn.Close()
	}
}

// stop ends the session or the connection of the link and tells its loop to
// quit. A stopped link is not reachable again.
func (l *link) stop() {
	l.mu.Lock()
	l.stopped = true
	sess := l.sess
	conn := l.conn
	l.sess = nil
	l.conn = nil
	l.online = false
	l.mu.Unlock()

	l.wake()
	if sess != nil {
		sess.Close()
		return
	}
	if conn != nil {
		_ = conn.Close()
	}
}

// isStopped reports whether the link stopped.
func (l *link) isStopped() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stopped
}

// needsUpgrade reports whether the link should replace its connection with one
// the local device initiates.
func (l *link) needsUpgrade() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.online && l.upgrade
}

// beginDial reserves the link for one outgoing attempt. A link that already has
// a connection, or is opening one, is left alone unless the attempt replaces a
// connection the other device initiated.
func (l *link) beginDial(upgrade bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dialing || l.stopped {
		return false
	}
	if upgrade {
		if !l.online {
			return false
		}
	} else if l.online || l.conn != nil {
		return false
	}
	l.dialing = true
	return true
}

// endDial releases the reservation of beginDial.
func (l *link) endDial() {
	l.mu.Lock()
	l.dialing = false
	l.mu.Unlock()
}

// takeConnection installs conn as the connection of the link and closes the
// previous one. It reports the generation of the new connection and whether the
// link was online before. A connection of the device with the higher device ID
// is refused when the link already has one.
func (l *link) takeConnection(local model.DeviceID, conn *transport.Conn, incoming bool) (uint64, bool, error) {
	l.mu.Lock()
	if l.conn != nil && !takesOver(local, l.id, incoming) {
		l.mu.Unlock()
		return 0, false, errSuperseded
	}
	previous := l.sess
	previousConn := l.conn
	wasOnline := l.online

	l.gen++
	gen := l.gen
	l.conn = conn
	l.sess = nil
	l.mu.Unlock()

	if previous != nil {
		previous.Close()
	} else if previousConn != nil {
		_ = previousConn.Close()
	}
	return gen, wasOnline, nil
}

// drop clears the connection of the generation after a failed handshake and
// reports whether the generation was still the current one.
func (l *link) drop(gen uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.gen != gen {
		return false
	}
	l.conn = nil
	l.sess = nil
	l.online = false
	return true
}

// block marks a peer that cannot be reached until the next network change.
func (l *link) block() {
	l.mu.Lock()
	l.blocked = true
	l.mu.Unlock()
}

// retry clears the backoff and wakes the link loop.
func (l *link) retry() {
	l.mu.Lock()
	l.attempts = 0
	l.blocked = false
	l.mu.Unlock()
	l.wake()
}

// wake tells the link loop to look at the link again.
func (l *link) wake() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// wait waits for the delay, a wake-up or the end of the node. A zero delay
// waits for the wake-up only.
func (l *link) wait(ctx context.Context, delay time.Duration) bool {
	var timer <-chan time.Time
	if delay > 0 {
		timer = time.After(delay)
	}
	select {
	case <-ctx.Done():
		return false
	case <-l.kick:
		return true
	case <-timer:
		return true
	}
}

// nextDelay returns the delay before the next attempt and counts the attempt.
func (l *link) nextDelay(first, limit time.Duration) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	delay := first
	for range l.attempts {
		if delay >= limit {
			break
		}
		delay *= backoffFactor
	}
	if delay > limit {
		delay = limit
	}
	l.attempts++
	return delay
}

// takesOver reports whether a new connection replaces the current one of the
// link. Both devices of a simultaneous dial settle on the connection the device
// with the lower device ID initiated.
func takesOver(local, peer model.DeviceID, incoming bool) bool {
	if incoming {
		return bytes.Compare(peer[:], local[:]) < 0
	}
	return bytes.Compare(local[:], peer[:]) < 0
}

// shouldUpgrade reports whether the local device has to replace a connection
// with its own one: it is the device with the lower device ID, but the other
// device initiated the connection.
func shouldUpgrade(local, peer model.DeviceID, incoming bool) bool {
	return incoming && bytes.Compare(local[:], peer[:]) < 0
}

// acceptLoop serves the incoming connections until the node stops.
func (n *Node) acceptLoop(ctx context.Context) {
	for {
		conn, err := n.listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			if errors.Is(err, transport.ErrPeerRejected) {
				n.log.Debug("connection from an untrusted device refused", "error", err)
				continue
			}
			n.log.Warn("accepting a connection failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(acceptRetryDelay):
			}
			continue
		}
		incoming := conn
		n.spawn(func() {
			if err := n.attach(ctx, incoming, true); err != nil && !errors.Is(err, errSuperseded) && ctx.Err() == nil {
				n.log.Debug("incoming connection dropped", "error", err)
			}
		})
	}
}

// browseLoop dials the devices discovery reports. Only a candidate that carries
// the group tag is dialed when discovery decides the match itself.
func (n *Node) browseLoop(ctx context.Context) {
	if tagged, ok := n.cfg.Discovery.(taggedDiscovery); ok {
		candidates, err := tagged.BrowseCandidates(ctx, ports.ServiceMain)
		if err != nil {
			n.log.Warn("browsing for devices failed", "error", err)
			return
		}
		consumeCandidates(ctx, candidates, func(candidate discovery.Candidate) {
			if !candidate.Matched {
				return
			}
			n.rememberCandidate(candidate.Addr)
			n.probe(ctx, candidate.Addr)
		})
		return
	}

	candidates, err := n.cfg.Discovery.Browse(ctx, ports.ServiceMain)
	if err != nil {
		n.log.Warn("browsing for devices failed", "error", err)
		return
	}
	consumeCandidates(ctx, candidates, func(candidate ports.Candidate) {
		n.rememberCandidate(candidate.Addr)
		n.probe(ctx, candidate.Addr)
	})
}

// consumeCandidates calls fn for every candidate until ctx ends or the channel
// closes.
func consumeCandidates[T any](ctx context.Context, candidates <-chan T, fn func(T)) {
	for {
		select {
		case <-ctx.Done():
			return
		case candidate, ok := <-candidates:
			if !ok {
				return
			}
			fn(candidate)
		}
	}
}

// rememberCandidate caches a discovered address for the next reconnect.
func (n *Node) rememberCandidate(addr string) {
	if addr == "" || len(addr) > maxAddrLength {
		return
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.candidates[addr]; !ok && len(n.candidates) >= maxCandidates {
		n.evictCandidateLocked()
	}
	n.candidates[addr] = candidate{seen: n.clock.Now()}
}

// evictCandidateLocked drops the oldest cached candidate. The caller must hold
// n.mu.
func (n *Node) evictCandidateLocked() {
	oldest := ""
	var seen time.Time
	for addr, candidate := range n.candidates {
		if oldest == "" || candidate.seen.Before(seen) {
			oldest, seen = addr, candidate.seen
		}
	}
	delete(n.candidates, oldest)
}

// probeCandidates dials every cached candidate. The handshake tells which
// device answers.
func (n *Node) probeCandidates(ctx context.Context) {
	for _, addr := range n.candidateAddrs() {
		n.probe(ctx, addr)
	}
}

// candidateAddrs returns the cached addresses that did not expire.
func (n *Node) candidateAddrs() []string {
	now := n.clock.Now()

	n.mu.Lock()
	defer n.mu.Unlock()
	addrs := make([]string, 0, len(n.candidates))
	for addr, candidate := range n.candidates {
		if now.Sub(candidate.seen) > candidateTTL {
			delete(n.candidates, addr)
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

// probe dials one discovered address when a peer is missing. A device that is
// not trusted is refused during the handshake.
func (n *Node) probe(ctx context.Context, addr string) {
	if ctx.Err() != nil || !n.needsPeers() {
		return
	}

	n.mu.Lock()
	if _, busy := n.probing[addr]; busy {
		n.mu.Unlock()
		return
	}
	n.probing[addr] = struct{}{}
	n.mu.Unlock()

	n.spawn(func() {
		defer func() {
			n.mu.Lock()
			delete(n.probing, addr)
			n.mu.Unlock()
		}()

		conn, err := n.dial(ctx, addr)
		if err != nil {
			n.log.Debug("discovered address did not answer", "addr", addr, "error", err)
			return
		}
		if err := n.attach(ctx, conn, false); err != nil && !errors.Is(err, errSuperseded) {
			n.log.Debug("discovered device refused", "addr", addr, "error", err)
		}
	})
}

// needsPeers reports whether a peer is offline and worth dialing.
func (n *Node) needsPeers() bool {
	for _, l := range n.linkList() {
		if online, blocked := l.status(); !online && !blocked {
			return true
		}
	}
	return false
}

// runLink keeps the link of one peer connected until the node stops.
func (n *Node) runLink(ctx context.Context, l *link) {
	for {
		if ctx.Err() != nil || l.isStopped() {
			return
		}
		online, blocked := l.status()
		if online {
			if l.needsUpgrade() {
				n.tryDial(ctx, l, true)
				if !l.needsUpgrade() {
					continue
				}
				if !l.wait(ctx, l.nextDelay(n.limits.ReconnectDelay, n.limits.ReconnectMaxDelay)) {
					return
				}
				continue
			}
			if !l.wait(ctx, 0) {
				return
			}
			continue
		}
		if blocked {
			if !l.wait(ctx, 0) {
				return
			}
			continue
		}
		n.tryDial(ctx, l, false)
		if online, _ := l.status(); online {
			continue
		}
		if !l.wait(ctx, l.nextDelay(n.limits.ReconnectDelay, n.limits.ReconnectMaxDelay)) {
			return
		}
	}
}

// tryDial connects to the peer through one of the known addresses: the cached
// ones first, then the addresses discovery reported. An upgrade replaces a
// connection that the other device initiated while this device should have
// initiated it.
func (n *Node) tryDial(ctx context.Context, l *link, upgrade bool) {
	if !l.beginDial(upgrade) {
		return
	}
	defer l.endDial()

	for _, addr := range n.dialAddrs(ctx, l.id) {
		if ctx.Err() != nil {
			return
		}
		conn, err := n.dial(ctx, addr)
		if err != nil {
			n.log.Debug("connecting to the peer failed", "peer", l.id, "addr", addr, "error", err)
			continue
		}
		if err := n.attach(ctx, conn, false); err != nil {
			if !errors.Is(err, errSuperseded) {
				n.log.Debug("peer connection refused", "peer", l.id, "addr", addr, "error", err)
			}
			continue
		}
		return
	}
}

// dialAddrs returns the addresses to try for the peer: its cached addresses and
// the discovered candidates.
func (n *Node) dialAddrs(ctx context.Context, id model.DeviceID) []string {
	cached := n.peerAddrs(ctx, id)
	addrs := make([]string, 0, len(cached)+len(n.candidateAddrs()))
	for _, addr := range append(cached, n.candidateAddrs()...) {
		if slices.Contains(addrs, addr) {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

// attach binds a connection to the link of its peer and opens the session. A
// connection of the device with the lower device ID wins a simultaneous dial.
func (n *Node) attach(ctx context.Context, conn *transport.Conn, incoming bool) error {
	if conn == nil {
		return errors.New("node: nil connection")
	}
	peer, err := n.trust.Get(ctx, conn.Peer().DeviceID())
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("node: peer lookup: %w", err)
	}
	if peer.Revoked {
		_ = conn.Close()
		return trust.ErrPeerRevoked
	}

	local := n.identity.DeviceID()
	l := n.startLink(peer)
	gen, wasOnline, err := l.takeConnection(local, conn, incoming)
	if err != nil {
		_ = conn.Close()
		return err
	}

	sess, err := n.openSession(ctx, conn)
	if err != nil {
		current := l.drop(gen)
		_ = conn.Close()
		if current && wasOnline {
			n.peerOffline(l, peer, err)
		}
		if errors.Is(err, session.ErrIncompatibleVersion) {
			if current {
				l.block()
			}
			version := uint32(0)
			if sess != nil {
				version = sess.PeerHello().Version
			}
			n.publish(Event{Kind: EventPeerIncompatible, Peer: l.id, Name: peer.Name, Version: version, Err: err})
			n.log.Info("peer needs an update", "peer", l.id, "version", version)
		}
		return err
	}

	l.mu.Lock()
	if l.gen != gen {
		l.mu.Unlock()
		sess.Close()
		return errSuperseded
	}
	l.sess = sess
	l.online = true
	l.attempts = 0
	l.blocked = false
	upgrade := shouldUpgrade(local, l.id, incoming)
	l.upgrade = upgrade
	l.mu.Unlock()

	n.recordAddr(ctx, peer.DeviceID(), addrOf(conn.RemoteAddr()), incoming)
	if err := n.trust.TouchSeen(ctx, l.id, n.clock.Now()); err != nil {
		n.log.Debug("recording the peer as seen failed", "peer", l.id, "error", err)
	}
	n.startHistorySync(sess)
	if !wasOnline {
		n.publish(Event{Kind: EventPeerOnline, Peer: l.id, Name: peer.Name, Role: peer.Role, Platform: peer.Platform, Path: model.PathLAN})
		n.log.Info("peer online", "peer", l.id, "role", peer.Role)
	}
	if upgrade {
		l.wake()
	}
	if n.gossip != nil {
		n.gossip.Opened(sess)
	}
	n.spawn(func() { n.watchSession(l, sess, gen) })
	return nil
}

// watchSession waits for the session to end and marks the peer offline.
func (n *Node) watchSession(l *link, sess *session.Session, gen uint64) {
	<-sess.Done()
	err := sess.Err()

	l.mu.Lock()
	if l.gen != gen {
		l.mu.Unlock()
		return
	}
	l.sess = nil
	l.conn = nil
	l.online = false
	peer := l.peer
	l.mu.Unlock()

	n.peerOffline(l, peer, err)
	l.wake()
	n.probeCandidates(n.ctx)
}

// peerOffline reports a peer that ended its session.
func (n *Node) peerOffline(l *link, peer trust.Peer, err error) {
	n.publish(Event{Kind: EventPeerOffline, Peer: l.id, Name: peer.Name, Role: peer.Role, Platform: peer.Platform, Err: err})
	if err != nil {
		n.log.Info("peer offline", "peer", l.id, "error", err)
		return
	}
	n.log.Info("peer offline", "peer", l.id)
}

// openSession creates the session, lets the hooks register their handlers and
// runs the handshake.
func (n *Node) openSession(ctx context.Context, conn *transport.Conn) (*session.Session, error) {
	require, err := n.trust.SelfRequireConfirmation(ctx)
	if err != nil {
		n.log.Warn("reading the local confirmation policy failed", "error", err)
	}

	sess, err := session.New(conn, session.Config{
		Name:                n.cfg.Name,
		Platform:            n.cfg.Platform,
		Capabilities:        n.cfg.Capabilities,
		RequireConfirmation: require,
		Trust:               n.trust,
		Clock:               n.clock,
		Logger:              n.log,
	})
	if err != nil {
		return nil, err
	}
	for _, hook := range n.sessionHooks() {
		if err := hook(ctx, sess); err != nil {
			return sess, fmt.Errorf("node: session hook: %w", err)
		}
	}
	if err := sess.Open(ctx); err != nil {
		return sess, err
	}
	return sess, nil
}

// sessionHooks returns a snapshot of the session hooks.
func (n *Node) sessionHooks() []SessionHook {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.hooks)
}

// recordAddr caches the address the peer was reached with. An address the peer
// connected from is an ephemeral port, so it is kept after the addresses that
// were dialed successfully.
func (n *Node) recordAddr(ctx context.Context, id model.DeviceID, addr string, incoming bool) {
	if addr == "" {
		return
	}
	if err := n.rememberAddr(ctx, id, addr, !incoming); err != nil {
		n.log.Debug("recording the peer address failed", "peer", id, "error", err)
	}
}

// rememberAddr adds addr to the cached addresses of the peer, at the front when
// front is set. The read and the write are serialized, so two connections of
// the same peer do not drop each other's address.
func (n *Node) rememberAddr(ctx context.Context, id model.DeviceID, addr string, front bool) error {
	n.addrMu.Lock()
	defer n.addrMu.Unlock()

	peer, err := n.trust.Get(ctx, id)
	if err != nil {
		return err
	}
	addrs := appendAddr(peer.LastAddrs, addr)
	if front {
		addrs = prependAddr(peer.LastAddrs, addr)
	}
	if slices.Equal(addrs, peer.LastAddrs) {
		return nil
	}
	return n.trust.UpdateAddrs(ctx, id, addrs)
}

// peerAddrs returns the cached addresses of the peer.
func (n *Node) peerAddrs(ctx context.Context, id model.DeviceID) []string {
	peer, err := n.trust.Get(ctx, id)
	if err != nil {
		n.log.Debug("reading the peer addresses failed", "peer", id, "error", err)
		return nil
	}
	return peer.LastAddrs
}

// dial opens an authenticated connection to addr.
func (n *Node) dial(ctx context.Context, addr string) (*transport.Conn, error) {
	target, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("node: address %q: %w", addr, err)
	}
	ctx, cancel := context.WithTimeout(ctx, n.limits.DialTimeout)
	defer cancel()
	return n.dialer.Dial(ctx, target)
}

// prependAddr returns addrs with addr first, without duplicates.
func prependAddr(addrs []string, addr string) []string {
	kept := make([]string, 0, len(addrs)+1)
	kept = append(kept, addr)
	for _, known := range addrs {
		if known == addr {
			continue
		}
		kept = append(kept, known)
	}
	return kept
}

// appendAddr returns addrs with addr last, without duplicates.
func appendAddr(addrs []string, addr string) []string {
	if slices.Contains(addrs, addr) {
		return addrs
	}
	kept := make([]string, 0, len(addrs)+1)
	kept = append(kept, addrs...)
	return append(kept, addr)
}

// addrOf returns the text form of addr.
func addrOf(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

// portOf returns the port of addr, or zero when it has none.
func portOf(addr net.Addr) int {
	if addr == nil {
		return 0
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return value
}
