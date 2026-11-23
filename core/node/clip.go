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

// clipQueueSize bounds the announced items that wait for their turn. The rate
// limit of announcements is far below the size of the queue.
const clipQueueSize = 64

// clipJob is one announced clipboard item that waits to be applied.
type clipJob struct {
	from model.DeviceID
	item model.ClipItem
}

// clipHandler queues the clipboard items a peer announces. Applying an item may
// download its payload, and the answer to the availability query comes over the
// control stream the handler runs on, so the handler returns at once.
func (n *Node) clipHandler(sess *session.Session) session.Handler {
	return func(_ context.Context, msg *wirev1.ControlMessage) error {
		announce := msg.GetClipAnnounce()
		if announce == nil {
			return nil
		}
		item, err := protoconv.ClipItemFromProto(announce.GetItem())
		if err != nil {
			return fmt.Errorf("node: clip announcement: %w", err)
		}

		select {
		case n.incoming <- clipJob{from: sess.DeviceID(), item: item}:
		default:
			n.log.Warn("incoming clipboard queue is full", "id", item.ID.String(), "peer", sess.DeviceID())
		}
		return nil
	}
}

// clipIncoming applies the announced items in the order they arrived.
func (n *Node) clipIncoming(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-n.incoming:
			if err := n.clip.Apply(ctx, job.from, job.item); err != nil {
				if ctx.Err() != nil {
					return
				}
				n.log.Warn("applying an incoming clip item failed",
					"id", job.item.ID.String(), "peer", job.from, "error", err)
			}
		}
	}
}

// policyPlanner turns the clipboard policy into the outcome of the pipeline.
type policyPlanner struct {
	decider *policy.Decider
}

// Plan returns the outcome for an item announced by a peer. The confirmation
// flag of the peer is part of the decision of the policy.
func (p policyPlanner) Plan(ctx context.Context, from model.DeviceID, item model.ClipItem) (clip.Outcome, error) {
	result, err := p.decider.DecideItem(ctx, from, item, model.SourceClipboard)
	if err != nil {
		return clip.Outcome{}, err
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
