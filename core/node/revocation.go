package node

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Timeouts of the revocation delivery.
const (
	// selfRevocationTimeout bounds the delivery of one self revocation.
	selfRevocationTimeout = 5 * time.Second
	// selfRevocationWait bounds the wait for the peers to apply a self
	// revocation and close their sessions.
	selfRevocationWait = 3 * time.Second
	// revocationApplyTimeout bounds the application of a received revocation.
	// The record has to be applied even when the sender closes the session
	// right after sending it.
	revocationApplyTimeout = 5 * time.Second
)

// RevokePeer revokes a paired device. The local device signs the revocation and
// keeps it for the group gossip. A device of the Personal group is cut off and
// the discovery key of the group is rotated, so the device cannot find the
// group by its tag anymore. The call is idempotent.
func (n *Node) RevokePeer(ctx context.Context, id model.DeviceID) error {
	if err := n.usable(); err != nil {
		return err
	}
	if id.IsZero() {
		return fmt.Errorf("%w: empty device id", trust.ErrInvalidPeer)
	}
	if id == n.DeviceID() {
		return n.RevokeSelf(ctx)
	}

	peer, err := n.trust.Get(ctx, id)
	if err != nil {
		return err
	}
	if peer.Revoked {
		n.dropLink(id)
		return nil
	}

	revocation, err := trust.NewRevocation(n.identity, id, n.clock.Now())
	if err != nil {
		return err
	}
	if _, err := n.trust.AddRevocation(ctx, revocation); err != nil {
		return err
	}
	n.dropLink(id)

	var rotateErr error
	if peer.Role == model.RolePersonal {
		if _, err := n.trust.RotateDiscoveryKey(ctx); err != nil {
			rotateErr = fmt.Errorf("node: rotate the discovery key: %w", err)
		}
	}

	nodeCtx := n.runningContext()
	if n.gossip != nil {
		n.gossip.Notify(nodeCtx)
	}
	if rotateErr == nil && peer.Role == model.RolePersonal {
		n.advertise(nodeCtx)
	}
	n.log.Info("device revoked", "peer", id, "role", peer.Role)
	return rotateErr
}

// RevokeSelf leaves the Personal group. The local device signs a revocation of
// itself, sends it to the open sessions and then drops the group: its peers,
// their introductions, the group secrets and the relay access. A device that is
// offline learns about the revocation when it meets another device of the
// group.
func (n *Node) RevokeSelf(ctx context.Context) error {
	if err := n.usable(); err != nil {
		return err
	}

	revocation, err := trust.NewRevocation(n.identity, n.DeviceID(), n.clock.Now())
	if err != nil {
		return err
	}
	peers := n.sendRevocation(ctx, revocation)
	n.waitSessionsClosed(ctx, peers)

	if err := n.leaveGroup(ctx); err != nil {
		return err
	}
	n.log.Info("left the group")
	return nil
}

// sendRevocation sends a revocation over every open session of the group and
// returns the devices that received it.
func (n *Node) sendRevocation(ctx context.Context, revocation trust.Revocation) []model.DeviceID {
	message := &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_Revocation{Revocation: revocation.Proto()},
	}
	sent := make([]model.DeviceID, 0, 4)
	for _, l := range n.linkList() {
		sess, peer, ok := l.onlineSession()
		if !ok || peer.Role != model.RolePersonal {
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, selfRevocationTimeout)
		err := sess.Send(sendCtx, message)
		cancel()
		if err != nil {
			n.log.Debug("sending the revocation failed", "peer", l.id, "error", err)
			continue
		}
		sent = append(sent, l.id)
	}
	return sent
}

// waitSessionsClosed waits until the peers applied the revocation and closed
// their sessions. The leaving device keeps its connections until then, so the
// record is not lost with the connection.
func (n *Node) waitSessionsClosed(ctx context.Context, peers []model.DeviceID) {
	deadline := time.Now().Add(selfRevocationWait)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return
		}
		open := false
		for _, id := range peers {
			if l := n.lookup(id); l != nil {
				if _, _, ok := l.onlineSession(); ok {
					open = true
					break
				}
			}
		}
		if !open {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	n.log.Warn("the peers of the group did not answer the revocation")
}

// leaveGroup drops the Personal group from this device and closes the sessions
// of the group.
func (n *Node) leaveGroup(ctx context.Context) error {
	peers, err := n.trust.List(ctx, trust.ListFilter{Role: model.RolePersonal, IncludeRevoked: true})
	if err == nil {
		defer func() {
			for _, peer := range peers {
				n.dropLink(peer.DeviceID())
			}
		}()
	}
	return n.trust.LeaveGroup(ctx)
}

// handleRevokedPeer closes the connection to a device that another device of the
// group revoked.
func (n *Node) handleRevokedPeer(_ context.Context, id model.DeviceID) {
	n.dropLink(id)
	n.log.Info("device revoked by the group", "peer", id)
}

// handleSelfRevoked drops the group after another device revoked the local
// device.
func (n *Node) handleSelfRevoked(ctx context.Context) {
	if err := n.leaveGroup(ctx); err != nil {
		n.log.Warn("leaving the group failed", "error", err)
		return
	}
	n.log.Info("left the group after a revocation")
}

// installRevocation registers the handler of a revocation a peer sends directly
// instead of through the gossip digest.
func (n *Node) installRevocation(_ context.Context, sess *session.Session) error {
	return sess.Handle(session.KindRevocation, n.revocationHandler())
}

// revocationHandler applies a revocation a peer sent over its session.
func (n *Node) revocationHandler() session.Handler {
	return func(ctx context.Context, msg *wirev1.ControlMessage) error {
		revocation, err := trust.RevocationFromProto(msg.GetRevocation())
		if err != nil {
			return fmt.Errorf("node: revocation: %w", err)
		}
		applyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revocationApplyTimeout)
		defer cancel()
		revoked, err := n.trust.ApplyRevocation(applyCtx, n.DeviceID(), revocation)
		switch {
		case errors.Is(err, trust.ErrSelfRevocation):
			n.handleSelfRevoked(applyCtx)
			return nil
		case err != nil:
			return fmt.Errorf("node: revocation: %w", err)
		case revoked:
			n.handleRevokedPeer(applyCtx, revocation.Subject)
		}
		return nil
	}
}

// dropLink ends the connection to the device and forgets the link, so the
// connection manager stops reaching it.
func (n *Node) dropLink(id model.DeviceID) {
	n.mu.Lock()
	l := n.links[id]
	delete(n.links, id)
	n.mu.Unlock()

	if l != nil {
		l.stop()
	}
}

// runningContext returns the context of the running node.
func (n *Node) runningContext() context.Context {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ctx
}
