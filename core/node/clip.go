package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/session"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

var (
	_ clip.Announcer = (*Node)(nil)
	_ clip.EventSink = (*Node)(nil)
)

// Announce delivers a captured clipboard item to the online Personal peers. It
// is called by the pipeline for every item this device captures.
func (n *Node) Announce(ctx context.Context, item model.ClipItem) error {
	n.publish(Event{Kind: EventClipboard, Local: true, Item: item})

	msg := &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_ClipAnnounce{
			ClipAnnounce: &wirev1.ClipAnnounce{Item: protoconv.ClipItemToProto(item)},
		},
	}

	var (
		delivered int
		failures  []error
	)
	for _, l := range n.linkList() {
		sess, peer, ok := l.clipTarget()
		if !ok {
			continue
		}
		if err := sess.Send(ctx, msg); err != nil {
			failures = append(failures, fmt.Errorf("peer %s: %w", peer.DeviceID(), err))
			continue
		}
		delivered++
	}

	if delivered == 0 && len(failures) > 0 {
		return errors.Join(failures...)
	}
	for _, err := range failures {
		n.log.Warn("clipboard announcement failed", "error", err)
	}
	return nil
}

// Report delivers the outcome of an incoming item to the subscribers.
func (n *Node) Report(_ context.Context, event clip.Event) {
	kind := EventClipboard
	switch event.Kind {
	case clip.EventDownload:
		kind = EventClipDownload
	case clip.EventApply:
		kind = EventClipApply
	}
	n.publish(Event{Kind: kind, Peer: event.Peer, Item: event.Item})
}

// clipHandler applies the clipboard items a peer announces.
func (n *Node) clipHandler(sess *session.Session) session.Handler {
	return func(ctx context.Context, msg *wirev1.ControlMessage) error {
		announce := msg.GetClipAnnounce()
		if announce == nil {
			return nil
		}
		item, err := protoconv.ClipItemFromProto(announce.GetItem())
		if err != nil {
			return fmt.Errorf("node: clip announcement: %w", err)
		}

		if err := n.clip.Apply(ctx, sess.DeviceID(), item); err != nil {
			return fmt.Errorf("node: apply clip item: %w", err)
		}
		return nil
	}
}

// policyPlanner turns the clipboard policy into the outcome of the pipeline.
type policyPlanner struct {
	decider *policy.Decider
	// self reports whether the local device holds incoming items for
	// confirmation.
	self func(ctx context.Context) bool
}

// Plan returns the outcome for an item announced by a peer. An item the policy
// applies automatically waits for the user while the confirmation flag of the
// local device is on.
func (p policyPlanner) Plan(ctx context.Context, from model.DeviceID, item model.ClipItem) (clip.Outcome, error) {
	result, err := p.decider.DecideItem(ctx, from, item, model.SourceClipboard)
	if err != nil {
		return clip.Outcome{}, err
	}
	if p.self != nil && result.Action == policy.ActionAutoApply && p.self(ctx) {
		return clip.Outcome{Action: clip.ActionConfirm}, nil
	}
	return clip.Outcome{Action: actionOf(result.Action), Eager: result.Eager}, nil
}

// actionOf converts a policy action to the pipeline action.
func actionOf(action policy.Action) clip.Action {
	switch action {
	case policy.ActionAutoApply:
		return clip.ActionApply
	case policy.ActionConfirm:
		return clip.ActionConfirm
	case policy.ActionMetadataOnly:
		return clip.ActionMetadata
	default:
		return clip.ActionSkip
	}
}
