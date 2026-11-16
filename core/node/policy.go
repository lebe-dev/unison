package node

import (
	"context"
	"fmt"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// SetConfirmation sets a confirmation flag of this device. The flag of the local
// device itself covers every paired device; the flag of a Personal device
// covers what that device sends. The device learns the new flag when it is
// online, so both of them show the same state.
func (n *Node) SetConfirmation(ctx context.Context, id model.DeviceID, scope trust.Scope, require bool) error {
	if err := n.usable(); err != nil {
		return err
	}
	if id == n.DeviceID() {
		if err := n.trust.SetSelfRequireConfirmation(ctx, scope, require); err != nil {
			return err
		}
		n.log.Info("confirmation flag for every device changed", "scope", scope.String(), "require_confirmation", require)
		return nil
	}

	peer, err := n.personalPeer(ctx, id)
	if err != nil {
		return err
	}
	changed, err := n.trust.SetRequireConfirmation(ctx, id, scope, require)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	n.log.Info("confirmation flag changed", "peer", id, "scope", scope.String(), "require_confirmation", require)
	n.publishDeviceUpdated(peer)
	n.announcePolicyTo(ctx, id)
	return nil
}

// RequestPeerConfirmation asks a Personal device to change its confirmation
// flag for this device. The request is signed, so only the device itself
// changes its flag; it has to be online and answers with its new flags.
func (n *Node) RequestPeerConfirmation(ctx context.Context, id model.DeviceID, scope trust.Scope, require bool) error {
	if err := n.usable(); err != nil {
		return err
	}
	if id == n.DeviceID() {
		return fmt.Errorf("%w: this device is not a paired device", trust.ErrInvalidPeer)
	}
	peer, err := n.personalPeer(ctx, id)
	if err != nil {
		return err
	}
	policy, err := trust.NewSetPeerPolicy(n.identity, id, scope, require, n.clock.Now())
	if err != nil {
		return err
	}

	sess, ok := n.sessionOf(id)
	if !ok {
		return fmt.Errorf("%w: %s", ErrPeerOffline, id)
	}
	message := &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_SetPeerPolicy{SetPeerPolicy: policy.Proto()},
	}
	if err := sess.Send(ctx, message); err != nil {
		return fmt.Errorf("node: send the peer policy: %w", err)
	}
	n.log.Info("confirmation request sent", "peer", id, "scope", scope.String(), "require_confirmation", require)

	// The device answers with its flags; until then the request is shown as
	// done, so the switch does not jump back.
	changed, err := n.trust.SetConfirmRemote(ctx, id, peer.ConfirmRemote.With(scope, require))
	if err != nil {
		n.log.Warn("recording the requested confirmation flag failed", "peer", id, "error", err)
		return nil
	}
	if changed {
		n.publishDeviceUpdated(peer)
	}
	return nil
}

// personalPeer returns a stored, non-revoked Personal device, the only kind a
// confirmation flag applies to.
func (n *Node) personalPeer(ctx context.Context, id model.DeviceID) (trust.Peer, error) {
	peer, err := n.trust.Get(ctx, id)
	if err != nil {
		return trust.Peer{}, err
	}
	if peer.Revoked {
		return trust.Peer{}, trust.ErrPeerRevoked
	}
	if peer.Role != model.RolePersonal {
		return trust.Peer{}, fmt.Errorf("%w: confirmation applies to personal devices only", trust.ErrInvalidPeer)
	}
	return peer, nil
}

// sessionOf returns the open session with a device.
func (n *Node) sessionOf(id model.DeviceID) (*session.Session, bool) {
	l := n.lookup(id)
	if l == nil {
		return nil, false
	}
	sess, _, ok := l.onlineSession()
	return sess, ok
}

// installPolicy registers the handlers of the confirmation flags of Personal
// peers: the signed requests to change a flag and the reports of their flags.
func (n *Node) installPolicy(_ context.Context, sess *session.Session) error {
	if err := sess.Handle(session.KindSetPeerPolicy, n.setPeerPolicyHandler(sess)); err != nil {
		return err
	}
	return sess.Handle(session.KindPeerPolicy, n.peerPolicyHandler(sess))
}

// setPeerPolicyHandler applies a request of a peer to change the flag of this
// device for it, and answers with the flags that apply after it.
func (n *Node) setPeerPolicyHandler(sess *session.Session) session.Handler {
	return func(ctx context.Context, msg *wirev1.ControlMessage) error {
		policy, err := trust.SetPeerPolicyFromProto(msg.GetSetPeerPolicy())
		if err != nil {
			return fmt.Errorf("node: peer policy: %w", err)
		}
		from := sess.DeviceID()
		changed, err := n.trust.ApplySetPeerPolicy(ctx, n.DeviceID(), from, policy)
		if err != nil {
			// The peer still learns the flags that apply.
			n.announcePolicy(ctx, sess)
			return fmt.Errorf("node: peer policy: %w", err)
		}
		if changed {
			n.log.Info("confirmation flag changed by the peer",
				"peer", from, "scope", policy.Scope.String(), "require_confirmation", policy.RequireConfirmation)
			if peer, err := n.trust.Get(ctx, from); err == nil {
				n.publishDeviceUpdated(peer)
			}
		}
		n.announcePolicy(ctx, sess)
		return nil
	}
}

// peerPolicyHandler records the flags a peer reports for this device.
func (n *Node) peerPolicyHandler(sess *session.Session) session.Handler {
	return func(ctx context.Context, msg *wirev1.ControlMessage) error {
		from := sess.DeviceID()
		flags := trust.ConfirmationsFromProto(msg.GetPeerPolicy())
		changed, err := n.trust.SetConfirmRemote(ctx, from, flags)
		if err != nil {
			return fmt.Errorf("node: record the peer confirmation flags: %w", err)
		}
		if !changed {
			return nil
		}
		n.log.Info("peer confirmation flags changed",
			"peer", from, "clipboard", flags.Clipboard, "files", flags.Files)
		if peer, err := n.trust.Get(ctx, from); err == nil {
			n.publishDeviceUpdated(peer)
		}
		return nil
	}
}

// announcePolicyTo tells an online device the flags of this device for it.
func (n *Node) announcePolicyTo(ctx context.Context, id model.DeviceID) {
	sess, ok := n.sessionOf(id)
	if !ok {
		return
	}
	n.announcePolicy(ctx, sess)
}

// announcePolicy tells the peer of a session the flags of this device for it.
// A Friends peer has no flags, so it learns nothing. A failure only leaves the
// peer with an older view: the next session sends the flags again.
func (n *Node) announcePolicy(ctx context.Context, sess *session.Session) {
	id := sess.DeviceID()
	peer, err := n.trust.Get(ctx, id)
	if err != nil || peer.Revoked || peer.Role != model.RolePersonal {
		return
	}
	message := &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_PeerPolicy{PeerPolicy: peer.Confirm.PeerPolicyProto()},
	}
	if err := sess.Send(ctx, message); err != nil && sess.Err() == nil {
		n.log.Debug("sending the confirmation flags failed", "peer", id, "error", err)
	}
}

// publishDeviceUpdated reports changed confirmation flags of a device.
func (n *Node) publishDeviceUpdated(peer trust.Peer) {
	n.publish(Event{
		Kind:     EventDeviceUpdated,
		Peer:     peer.DeviceID(),
		Name:     peer.Name,
		Role:     peer.Role,
		Platform: peer.Platform,
		At:       n.clock.Now(),
	})
}

// selfRequireConfirmation reports whether this device holds the content of the
// scope of every peer. It fails closed: a flag that cannot be read asks the
// user.
func (n *Node) selfRequireConfirmation(ctx context.Context, scope trust.Scope) bool {
	require, err := n.trust.SelfRequireConfirmation(ctx, scope)
	if err != nil {
		n.log.Warn("reading the confirmation flag for every device failed", "scope", scope.String(), "error", err)
		return true
	}
	return require
}

// RelayFilesOnRequest reports whether files travel over the relay only when the
// user asks for them.
func (n *Node) RelayFilesOnRequest() bool {
	return n.policy.Config().RelayFilesOnRequest
}

// SetRelayFilesOnRequest changes the rule for files over the relay and applies it
// without a restart.
func (n *Node) SetRelayFilesOnRequest(ctx context.Context, on bool) error {
	if err := n.usable(); err != nil {
		return err
	}
	if err := n.Store().Settings().SetBool(ctx, policy.RelayFilesKey, on); err != nil {
		return err
	}
	cfg := n.policy.Config()
	cfg.RelayFilesOnRequest = on
	n.policy.SetConfig(cfg)
	n.log.Info("relay file rule changed", "files_on_request", on)
	return nil
}
