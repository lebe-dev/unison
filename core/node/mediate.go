package node

import (
	"context"
	"net"
	"net/netip"

	"github.com/lebe-dev/unison/core/mediate"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
)

// nodeSessions hands the open sessions to the mediate service.
type nodeSessions struct {
	node *Node
}

var _ mediate.Sessions = nodeSessions{}

// Session returns the open session with the device, or nil when it is offline.
func (s nodeSessions) Session(id model.DeviceID) *session.Session {
	sess, ok := s.node.sessionOf(id)
	if !ok {
		return nil
	}
	return sess
}

// mediateEvents publishes the events of the mediate service.
type mediateEvents struct {
	node *Node
}

var _ mediate.EventSink = mediateEvents{}

// Report delivers one exchange event to the subscribers.
func (m mediateEvents) Report(_ context.Context, event mediate.Event) {
	kind := EventMediatedPairUpdated
	switch event.Kind {
	case mediate.EventOffer:
		kind = EventMediatedPairOffer
	case mediate.EventCompleted:
		kind = EventMediatedPairCompleted
	case mediate.EventCanceled:
		kind = EventMediatedPairCanceled
	}
	exchange := event.Exchange
	m.node.publish(Event{Kind: kind, Mediated: &exchange})
}

// openMediate creates the mediated pairing service of the node.
func (n *Node) openMediate() error {
	service, err := mediate.New(mediate.Options{
		Identity:    n.identity,
		Trust:       n.trust,
		Sessions:    nodeSessions{node: n},
		Name:        n.Name(),
		Platform:    n.cfg.Platform,
		ListenAddrs: n.ListenAddrs,
		LoopbackHint: func(id model.DeviceID) bool {
			return n.sessionIsLoopback(id)
		},
		Events:    mediateEvents{node: n},
		Completed: n.mediatedPairCompleted,
		Clock:     n.clock,
		Logger:    n.log,
	})
	if err != nil {
		return err
	}
	n.mediate = service
	return nil
}

// installMediate registers the mediated pairing handlers on a new session.
func (n *Node) installMediate(_ context.Context, sess *session.Session) error {
	return n.mediate.Register(sess)
}

// StartMediatedPair introduces two online devices to each other. This device is
// the mediator: it knows both of them and the user picks the role of the new
// pair.
func (n *Node) StartMediatedPair(ctx context.Context, peerA, peerB model.DeviceID, role model.Role) (mediate.Exchange, error) {
	if err := n.usable(); err != nil {
		return mediate.Exchange{}, err
	}
	return n.mediate.Start(ctx, peerA, peerB, role)
}

// ConfirmMediatedPair accepts an incoming offer. enableFriendsDiscovery turns
// the search for Friends devices on when the pair completes.
func (n *Node) ConfirmMediatedPair(ctx context.Context, sessionID string, enableFriendsDiscovery bool) error {
	if err := n.usable(); err != nil {
		return err
	}
	return n.mediate.Confirm(ctx, sessionID, enableFriendsDiscovery)
}

// RejectMediatedPair declines an incoming offer.
func (n *Node) RejectMediatedPair(ctx context.Context, sessionID string) error {
	if err := n.usable(); err != nil {
		return err
	}
	return n.mediate.Reject(ctx, sessionID)
}

// CancelMediatedPair ends an exchange this device started or takes part in.
func (n *Node) CancelMediatedPair(ctx context.Context, sessionID string) error {
	if err := n.usable(); err != nil {
		return err
	}
	return n.mediate.Cancel(ctx, sessionID)
}

// MediatedPairs returns the active mediated pairing exchanges.
func (n *Node) MediatedPairs() []mediate.Exchange {
	if n.mediate == nil {
		return nil
	}
	return n.mediate.Exchanges()
}

// Mediate returns the mediated pairing service. It is nil before Start.
func (n *Node) Mediate() *mediate.Service {
	return n.mediate
}

// MediatedPeerSupported reports that the open session with the device
// announced the mediated pairing capability.
func (n *Node) MediatedPeerSupported(id model.DeviceID) bool {
	sess, ok := n.sessionOf(id)
	return ok && sess.HasCapability(mediate.Capability)
}

// mediatedPairCompleted connects the new pair and, when the user asked for it,
// turns the search for Friends devices on.
func (n *Node) mediatedPairCompleted(ctx context.Context, exchange mediate.Exchange) {
	if exchange.Role == model.RoleFriends && exchange.EnableFriendsDiscovery {
		if _, err := n.SetFriendsDiscovery(ctx, true); err != nil {
			n.log.Warn("turning the friends discovery on failed", "error", err)
		}
	}
	n.syncLinks()
	if exchange.Role == model.RolePersonal && n.gossip != nil {
		n.gossip.Notify(ctx)
	}
}

// sessionIsLoopback reports whether the session with the device runs over
// loopback, which is the case on a development machine where two devices share
// one host.
func (n *Node) sessionIsLoopback(id model.DeviceID) bool {
	link := n.lookup(id)
	if link == nil {
		return false
	}
	link.mu.Lock()
	conn := link.conn
	link.mu.Unlock()
	if conn == nil {
		return false
	}
	return addrIsLoopback(conn.RemoteAddr())
}

// addrIsLoopback reports whether a network address is a loopback endpoint.
func addrIsLoopback(addr net.Addr) bool {
	if addr == nil {
		return false
	}
	parsed, err := netip.ParseAddrPort(addrOf(addr))
	return err == nil && parsed.Addr().IsLoopback()
}
