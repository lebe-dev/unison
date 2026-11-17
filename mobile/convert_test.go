package mobile

import (
	"testing"

	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
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

// TestContentToPayloadCarriesTheItemID checks that the host learns which item
// it writes, so a keyboard can tell the write from a copy of the user.
func TestContentToPayloadCarriesTheItemID(t *testing.T) {
	content := ports.Content{
		ItemID:          "0f8fad5b-d9cb-469f-a165-70867728950e",
		Representations: []ports.Representation{{MIME: "text/plain", Data: []byte("hello")}},
	}

	payload := contentToPayload(content)

	if got := payload.GetItemId(); got != content.ItemID {
		t.Fatalf("payload names item %q, want %q", got, content.ItemID)
	}
}
