package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/session"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

var _ clip.Announcer = (*Node)(nil)

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

		from := sess.DeviceID()
		if err := n.clip.Apply(ctx, from, item); err != nil {
			return fmt.Errorf("node: apply clip item: %w", err)
		}
		n.publish(Event{Kind: EventClipboard, Peer: from, Item: item})
		return nil
	}
}
