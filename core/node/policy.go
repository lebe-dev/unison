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

// SetPeerRequireConfirmation sets the confirmation flag of a device. The local
// device changes its own flag directly and asks another device to change its
// flag with a signed policy, so the flag of a device is only ever changed by
// that device.
func (n *Node) SetPeerRequireConfirmation(ctx context.Context, id model.DeviceID, require bool) error {
	if err := n.usable(); err != nil {
		return err
	}
	if id == n.DeviceID() {
		return n.trust.SetSelfRequireConfirmation(ctx, require)
	}

	peer, err := n.trust.Get(ctx, id)
	if err != nil {
		return err
	}
	if peer.Revoked {
		return trust.ErrPeerRevoked
	}
	if peer.Role != model.RolePersonal {
		return fmt.Errorf("%w: confirmation applies to personal devices only", trust.ErrInvalidPeer)
	}
	policy, err := trust.NewSetPeerPolicy(n.identity, id, require, n.clock.Now())
	if err != nil {
		return err
	}

	l := n.lookup(id)
	if l == nil {
		return fmt.Errorf("%w: %s", ErrPeerOffline, id)
	}
	sess, _, ok := l.onlineSession()
	if !ok {
		return fmt.Errorf("%w: %s", ErrPeerOffline, id)
	}
	message := &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_SetPeerPolicy{SetPeerPolicy: policy.Proto()},
	}
	if err := sess.Send(ctx, message); err != nil {
		return fmt.Errorf("node: send the peer policy: %w", err)
	}
	n.log.Info("confirmation policy sent", "peer", id, "require_confirmation", require)
	return nil
}

// installPolicy registers the handler that applies the confirmation policies of
// Personal peers.
func (n *Node) installPolicy(_ context.Context, sess *session.Session) error {
	return sess.Handle(session.KindSetPeerPolicy, n.setPeerPolicyHandler(sess))
}

// setPeerPolicyHandler applies a policy a peer sent to this device.
func (n *Node) setPeerPolicyHandler(sess *session.Session) session.Handler {
	return func(ctx context.Context, msg *wirev1.ControlMessage) error {
		policy, err := trust.SetPeerPolicyFromProto(msg.GetSetPeerPolicy())
		if err != nil {
			return fmt.Errorf("node: peer policy: %w", err)
		}
		changed, err := n.trust.ApplySetPeerPolicy(ctx, n.DeviceID(), sess.DeviceID(), policy)
		if err != nil {
			return fmt.Errorf("node: peer policy: %w", err)
		}
		if changed {
			n.log.Info("confirmation flag changed", "peer", sess.DeviceID(), "require_confirmation", policy.RequireConfirmation)
		}
		return nil
	}
}

// selfRequireConfirmation reports whether this device holds incoming items for
// confirmation. It fails closed: a policy that cannot be read asks the user.
func (n *Node) selfRequireConfirmation(ctx context.Context) bool {
	require, err := n.trust.SelfRequireConfirmation(ctx)
	if err != nil {
		n.log.Warn("reading the local confirmation policy failed", "error", err)
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
