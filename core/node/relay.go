package node

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/relay"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/trust"
)

// errRelayOff reports a device without relay access.
var errRelayOff = errors.New("node: relay is not configured")

// relayAllowed reports whether the policy allows relay traffic on the current
// network.
func (n *Node) relayAllowed() bool {
	row, ok := n.policy.Config().Row(policy.ConditionOf(n.Network()))
	return ok && row.Relay
}

// relayDue reports whether the link should open the relay path: the peer stayed
// off the local network for the relay delay and the policy allows relay.
func (n *Node) relayDue(l *link) bool {
	if !n.relayAllowed() {
		return false
	}
	offline := l.offlineAt()
	if offline.IsZero() {
		return true
	}
	return n.clock.Now().Sub(offline) >= n.limits.RelayDelay
}

// relayClient returns the relay client of the stored group settings. It creates
// the client on the first call and replaces it when the settings change.
func (n *Node) relayClient(ctx context.Context) (*relay.Dialer, error) {
	data, err := n.trust.GroupData(ctx)
	if err != nil {
		return nil, err
	}
	if data.Relay.Empty() {
		return nil, errRelayOff
	}

	cfg := n.cfg.Relay
	cfg.URL = data.Relay.URL
	cfg.Token = data.Relay.Token
	cfg.Clock = n.clock
	if cfg.Logger == nil {
		cfg.Logger = n.log
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = n.limits.ReconnectDelay
	}
	if cfg.ReconnectLimit <= 0 {
		cfg.ReconnectLimit = n.limits.ReconnectMaxDelay
	}

	n.relayMu.Lock()
	defer n.relayMu.Unlock()
	if n.relay != nil && n.relayConfig.URL == cfg.URL && n.relayConfig.Token == cfg.Token && n.relayConfig.QUICAddr == cfg.QUICAddr {
		return n.relay, nil
	}
	n.closeRelayLocked()

	n.mu.Lock()
	base := n.ctx
	n.mu.Unlock()

	client, err := relay.NewDialer(cfg)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(base)
	n.relay = client
	n.relayConfig = cfg
	n.relayCancel = cancel
	n.spawn(func() {
		if err := client.Run(runCtx); err != nil {
			n.log.Debug("relay client stopped", "error", err)
		}
	})
	return client, nil
}

// closeRelayLocked stops the relay client. The caller must hold relayMu.
func (n *Node) closeRelayLocked() {
	if n.relayCancel != nil {
		n.relayCancel()
		n.relayCancel = nil
	}
	if n.relay != nil {
		if err := n.relay.Close(); err != nil {
			n.log.Debug("closing the relay client failed", "error", err)
		}
		n.relay = nil
	}
	n.relayConfig = relay.Config{}
}

// relaySettingsChanged drops a relay client whose settings are outdated and
// wakes the links that wait for the relay.
func (n *Node) relaySettingsChanged(ctx context.Context) {
	data, err := n.trust.GroupData(ctx)
	if err != nil {
		return
	}

	n.relayMu.Lock()
	stale := n.relay != nil && (n.relayConfig.URL != data.Relay.URL || n.relayConfig.Token != data.Relay.Token)
	if stale {
		n.closeRelayLocked()
	}
	n.relayMu.Unlock()

	if stale {
		n.wakeRelay()
	}
}

// relayNetworkChanged reports a network change to the relay client.
func (n *Node) relayNetworkChanged() {
	n.relayMu.Lock()
	client := n.relay
	n.relayMu.Unlock()

	if client != nil {
		client.NetworkChanged()
	}
}

// relayStatus returns the state of the relay client.
func (n *Node) relayStatus() relay.Status {
	n.relayMu.Lock()
	client := n.relay
	n.relayMu.Unlock()

	if client == nil {
		return relay.Status{}
	}
	return client.Status()
}

// wakeRelay asks the links that wait for the relay to try it again.
func (n *Node) wakeRelay() {
	select {
	case n.relayKick <- struct{}{}:
	default:
	}
}

// startRelay opens the relay path of the peer unless it already runs.
func (n *Node) startRelay(ctx context.Context, l *link) {
	relayCtx, cancel := context.WithCancel(ctx)
	if !l.beginRelay(cancel) {
		cancel()
		return
	}
	n.spawn(func() { n.relayLoop(relayCtx, l) })
}

// relayPath is the relay connection of one peer: the mailbox of the pair and
// the QUIC transport that carries the peer connection.
type relayPath struct {
	client   *relay.Dialer
	shared   *transport.Shared
	listener *transport.Listener
	dialer   *transport.Dialer
}

// close releases the relay path and its mailbox.
func (p *relayPath) close() {
	_ = p.shared.Close()
}

// relayLoop keeps the relay path of one peer until the link leaves it, the node
// stops, or the peer becomes reachable in the local network. The mailbox stays
// registered across failed attempts, so the relay connection is not disturbed.
func (n *Node) relayLoop(ctx context.Context, l *link) {
	defer l.endRelay()

	delay := n.limits.ReconnectDelay
	warned := false
	for {
		if ctx.Err() != nil || !l.relayWanted() {
			return
		}

		path, err := n.openRelayPath(ctx, l)
		if err == nil {
			n.serveRelayPath(ctx, l, path)
			path.close()
			delay = n.limits.ReconnectDelay
			warned = false
			continue
		}
		if ctx.Err() != nil || !l.relayWanted() {
			return
		}
		switch {
		case errors.Is(err, errRelayOff):
			n.log.Debug("no relay settings for the device", "peer", l.id)
		case errors.Is(err, relay.ErrDatagramTooSmall):
			if !warned {
				n.log.Warn("relay datagrams cannot carry QUIC, the relay path stays off", "error", err)
				warned = true
			}
		default:
			n.log.Debug("relay path failed", "peer", l.id, "error", err)
		}
		if !n.pauseRelay(ctx, delay) {
			return
		}
		delay = min(delay*2, n.limits.ReconnectMaxDelay)
	}
}

// serveRelayPath keeps one relay path alive: it opens the peer connection again
// after the session ended, without registering the mailbox again.
func (n *Node) serveRelayPath(ctx context.Context, l *link, path *relayPath) {
	delay := n.limits.ReconnectDelay
	for {
		if !n.waitRelay(ctx, path.client) || !l.relayWanted() {
			return
		}

		conn, incoming, err := n.relayConnect(ctx, l, path.listener, path.dialer)
		if err != nil {
			if ctx.Err() != nil || !l.relayWanted() {
				return
			}
			n.log.Debug("relay connection failed", "peer", l.id, "error", err)
			if !n.pauseRelay(ctx, delay) {
				return
			}
			delay = min(delay*2, n.limits.ReconnectMaxDelay)
			continue
		}
		sess, err := n.attach(ctx, conn, incoming)
		if err != nil {
			if ctx.Err() != nil || !l.relayWanted() {
				return
			}
			n.log.Debug("relay session failed", "peer", l.id, "error", err)
			if !n.pauseRelay(ctx, delay) {
				return
			}
			delay = min(delay*2, n.limits.ReconnectMaxDelay)
			continue
		}

		delay = n.limits.ReconnectDelay
		n.watchRelay(ctx, l.id, sess, path.client)
		if ctx.Err() != nil || !l.relayWanted() {
			return
		}
		if !n.pauseRelay(ctx, delay) {
			return
		}
	}
}

// pauseRelay waits before the next relay attempt of the link.
func (n *Node) pauseRelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-n.relayKick:
		return true
	case <-timer.C:
		return true
	}
}

// openRelayPath registers the mailbox of the pair and binds the QUIC transport
// of the peer connection to it.
func (n *Node) openRelayPath(ctx context.Context, l *link) (*relayPath, error) {
	client, err := n.relayClient(ctx)
	if err != nil {
		return nil, err
	}
	key, err := n.rendezvousKey(ctx, l.id)
	if err != nil {
		return nil, err
	}

	pc, err := client.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	shared, err := transport.NewShared(pc)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}

	size := client.PacketSize()
	if size < transport.MinPacketSize {
		_ = shared.Close()
		return nil, relay.ErrDatagramTooSmall
	}
	cfg := n.cfg.Transport
	cfg.PacketSize = size

	verifier := peerVerifier{trust: n.trust}
	listener, err := shared.Listener(n.identity, verifier, cfg)
	if err != nil {
		_ = shared.Close()
		return nil, err
	}
	dialer, err := shared.Dialer(n.identity, verifier, cfg)
	if err != nil {
		_ = shared.Close()
		return nil, err
	}
	return &relayPath{client: client, shared: shared, listener: listener, dialer: dialer}, nil
}

// watchRelay serves a relay session until the peer connection ends or the relay
// stays away for longer than the grace period. A short outage does not end the
// session, since QUIC retransmits the lost packets once the relay is back.
func (n *Node) watchRelay(ctx context.Context, id model.DeviceID, sess *session.Session, client *relay.Dialer) {
	for {
		changes := client.Changes()
		if client.Status().Connected {
			select {
			case <-ctx.Done():
				return
			case <-sess.Done():
				return
			case <-changes:
			}
			continue
		}

		timer := time.NewTimer(n.limits.RelayGrace)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-sess.Done():
			timer.Stop()
			return
		case <-changes:
			timer.Stop()
		case <-timer.C:
			// The relay stayed away, so the session cannot continue.
			n.log.Debug("relay path dropped after an outage", "peer", id)
			sess.Close()
			return
		}
	}
}

// waitRelay waits until the relay client has a connection.
func (n *Node) waitRelay(ctx context.Context, client *relay.Dialer) bool {
	for {
		changes := client.Changes()
		if client.Status().Connected {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-changes:
		}
	}
}

// relayConnect opens the relay connection of the peer. Only the device with the
// lower device ID dials, so the two sides do not open two connections at once.
func (n *Node) relayConnect(ctx context.Context, l *link, listener *transport.Listener, dialer *transport.Dialer) (*transport.Conn, bool, error) {
	local := n.identity.DeviceID()
	if bytes.Compare(local[:], l.id[:]) >= 0 {
		conn, err := listener.Accept(ctx)
		return conn, true, err
	}

	ctx, cancel := context.WithTimeout(ctx, n.limits.DialTimeout)
	defer cancel()
	conn, err := dialer.Dial(ctx, listener.Addr())
	return conn, false, err
}

// rendezvousKey returns the mailbox secret of the pair. A pair that never
// exchanged one derives it from both device IDs.
func (n *Node) rendezvousKey(ctx context.Context, id model.DeviceID) ([]byte, error) {
	key, err := n.trust.RendezvousKey(ctx, id)
	switch {
	case err == nil:
		return key, nil
	case errors.Is(err, trust.ErrSecretNotFound):
		return trust.DerivedRendezvousKey(n.identity.DeviceID(), id), nil
	default:
		return nil, err
	}
}
