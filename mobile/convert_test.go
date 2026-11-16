package mobile

import (
	"testing"

	"github.com/lebe-dev/unison/core/node"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestEventKindToProto checks that every node event keeps its kind on the way
// to the host, which selects the notification by it.
func TestEventKindToProto(t *testing.T) {
	cases := map[node.EventKind]localv1.EventKind{
		node.EventPeerOnline:       localv1.EventKind_EVENT_KIND_PEER_ONLINE,
		node.EventPeerOffline:      localv1.EventKind_EVENT_KIND_PEER_OFFLINE,
		node.EventClipboard:        localv1.EventKind_EVENT_KIND_CLIPBOARD,
		node.EventClipDownload:     localv1.EventKind_EVENT_KIND_CLIP_DOWNLOAD,
		node.EventClipApply:        localv1.EventKind_EVENT_KIND_CLIP_APPLY,
		node.EventTransfer:         localv1.EventKind_EVENT_KIND_TRANSFER,
		node.EventPeerIncompatible: localv1.EventKind_EVENT_KIND_PEER_INCOMPATIBLE,
		node.EventPairingCandidate: localv1.EventKind_EVENT_KIND_PAIRING_CANDIDATE,
		node.EventPairingRequest:   localv1.EventKind_EVENT_KIND_PAIRING_REQUEST,
		node.EventPairingPaired:    localv1.EventKind_EVENT_KIND_PAIRING_PAIRED,
		node.EventPairingFailed:    localv1.EventKind_EVENT_KIND_PAIRING_FAILED,
		node.EventPairingClosed:    localv1.EventKind_EVENT_KIND_PAIRING_CLOSED,
	}
	for kind, want := range cases {
		if got := eventKindToProto(kind); got != want {
			t.Errorf("eventKindToProto(%v) = %v, want %v", kind, got, want)
		}
	}
}
