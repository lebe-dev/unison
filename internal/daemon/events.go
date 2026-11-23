package daemon

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
)

// logEvents writes the events of the node to the log until ctx is done.
func logEvents(ctx context.Context, log *slog.Logger, subscriber *node.Subscriber) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-subscriber.Events():
			if !ok {
				return
			}
			logEvent(log, event)
		}
	}
}

// logEvent writes one node event. Clipboard content never reaches the log: an
// item is described by its identifier, its kind, its size and its types.
func logEvent(log *slog.Logger, event node.Event) {
	switch event.Kind {
	case node.EventPeerOnline:
		log.Info("peer online",
			"peer", deviceID(event.Peer),
			"name", event.Name,
			"role", event.Role.String(),
			"path", event.Path.String(),
		)
	case node.EventPeerOffline:
		attrs := []any{"peer", deviceID(event.Peer), "name", event.Name}
		attrs = append(attrs, errorAttr(event.Err)...)
		log.Info("peer offline", attrs...)
	case node.EventPeerIncompatible:
		message := "peer runs an older protocol version and needs an update"
		if event.ProtocolNewer {
			message = "peer runs a newer protocol version, this device needs an update"
		}
		log.Warn(message,
			"peer", deviceID(event.Peer),
			"version", event.Version,
		)
	case node.EventClipboard:
		attrs := []any{
			"id", event.Item.ID.String(),
			"local", event.Local,
			"kind", event.Item.Kind.String(),
			"bytes", itemSize(event.Item),
			"types", itemTypes(event.Item),
		}
		if !event.Peer.IsZero() {
			attrs = append(attrs, "peer", event.Peer.String())
		}
		log.Info("clipboard item", attrs...)
	case node.EventPairingCandidate:
		log.Debug("pairing candidate", "addr", event.Addr)
	case node.EventPairingRequest:
		log.Info("pairing request", "peer", deviceID(event.Peer), "name", event.Name, "addr", event.Addr)
	case node.EventPairingPaired:
		log.Info("pairing finished", "peer", deviceID(event.Peer), "name", event.Name, "role", event.Role.String())
	case node.EventPairingFailed:
		attrs := []any{"peer", deviceID(event.Peer)}
		attrs = append(attrs, errorAttr(event.Err)...)
		log.Warn("pairing failed", attrs...)
	case node.EventPairingClosed:
		log.Info("pairing window closed")
	default:
		log.Debug("node event", "event", event.Kind.String(), "peer", deviceID(event.Peer))
	}
}

// deviceID returns the device ID as a string, or an empty string for the zero
// ID.
func deviceID(id model.DeviceID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}

// itemSize returns the size of the payloads of an item.
func itemSize(item model.ClipItem) uint64 {
	var size uint64
	for _, rep := range item.Representations {
		size += rep.Size
	}
	return size
}

// itemTypes returns the MIME types of an item.
func itemTypes(item model.ClipItem) string {
	types := make([]string, 0, len(item.Representations))
	for _, rep := range item.Representations {
		types = append(types, rep.MIME)
	}
	return strings.Join(types, ",")
}

// errorAttr returns the attribute of an error, or nothing when there is none.
func errorAttr(err error) []any {
	if err == nil {
		return nil
	}
	return []any{"error", err}
}
