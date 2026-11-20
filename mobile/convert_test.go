package mobile

import (
	"fmt"
	"testing"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/ports"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestEventKindToProto checks that every node event keeps its kind on the way
// to the host, which selects the notification by it.
func TestEventKindToProto(t *testing.T) {
	cases := map[node.EventKind]localv1.EventKind{
		node.EventPeerOnline:       localv1.EventKind_EVENT_KIND_PEER_ONLINE,
		node.EventPeerUnreachable:  localv1.EventKind_EVENT_KIND_PEER_UNREACHABLE,
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

// TestLocalEventFailureReason checks that the host gets the reason of an
// unreachable device next to the technical error.
func TestLocalEventFailureReason(t *testing.T) {
	event := localEventToProto(node.Event{
		Kind: node.EventPairingFailed,
		Err:  fmt.Errorf("%w: 192.168.1.2:4000: timeout", pairing.ErrUnreachable),
	})
	if event.GetFailureReason() != localv1.FailureReason_FAILURE_REASON_UNREACHABLE || event.GetError() == "" {
		t.Errorf("event = %v, want an unreachable failure with details", event)
	}
	if got := localEventToProto(node.Event{Kind: node.EventPairingFailed, Err: pairing.ErrRejected}); got.GetFailureReason() != localv1.FailureReason_FAILURE_REASON_UNSPECIFIED {
		t.Errorf("reason of a refusal = %v, want none", got.GetFailureReason())
	}
}

func TestPlatformToProto(t *testing.T) {
	tests := map[string]struct {
		platform model.Platform
		want     localv1.Platform
	}{
		"macos":   {platform: model.PlatformMacOS, want: localv1.Platform_PLATFORM_MACOS},
		"windows": {platform: model.PlatformWindows, want: localv1.Platform_PLATFORM_WINDOWS},
		"android": {platform: model.PlatformAndroid, want: localv1.Platform_PLATFORM_ANDROID},
		"linux":   {platform: model.PlatformLinux, want: localv1.Platform_PLATFORM_LINUX},
		"unknown": {platform: model.PlatformUnknown, want: localv1.Platform_PLATFORM_UNSPECIFIED},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := platformToProto(test.platform); got != test.want {
				t.Fatalf("platformToProto = %v, want %v", got, test.want)
			}
		})
	}
}
