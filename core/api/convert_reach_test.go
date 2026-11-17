package api

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

func TestEventToProtoFailureReason(t *testing.T) {
	unreachable := eventToProto(node.Event{
		Kind: node.EventPairingFailed,
		Err:  fmt.Errorf("%w: 192.168.1.2:4000: timeout", pairing.ErrUnreachable),
	})
	if unreachable.GetFailureReason() != localv1.FailureReason_FAILURE_REASON_UNREACHABLE {
		t.Errorf("reason = %v, want unreachable", unreachable.GetFailureReason())
	}
	if unreachable.GetError() == "" {
		t.Error("the technical details are missing")
	}

	peer := eventToProto(node.Event{Kind: node.EventPeerUnreachable, Err: errors.New("192.168.1.2:4000: timeout")})
	if peer.GetKind() != localv1.EventKind_EVENT_KIND_PEER_UNREACHABLE ||
		peer.GetFailureReason() != localv1.FailureReason_FAILURE_REASON_UNREACHABLE {
		t.Errorf("peer event = %v, want an unreachable peer", peer)
	}

	rejected := eventToProto(node.Event{Kind: node.EventPairingFailed, Err: pairing.ErrRejected})
	if rejected.GetFailureReason() != localv1.FailureReason_FAILURE_REASON_UNSPECIFIED {
		t.Errorf("reason of a refusal = %v, want none", rejected.GetFailureReason())
	}
}

func TestDeviceToProtoUnreachable(t *testing.T) {
	since := time.UnixMilli(1_700_000_000_000)
	device := deviceToProto(trust.Peer{Name: "phone"}, model.PeerState{
		Unreachable:       true,
		UnreachableSince:  since,
		UnreachableDetail: "192.168.1.2:4000: timeout",
	})
	if !device.GetUnreachable() || device.GetUnreachableSinceUnixMs() != since.UnixMilli() ||
		device.GetUnreachableDetail() != "192.168.1.2:4000: timeout" {
		t.Errorf("device = %v, want the unreachable state", device)
	}
}
