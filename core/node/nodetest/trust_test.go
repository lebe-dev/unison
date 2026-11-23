package nodetest

import (
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
)

// TestUnknownDeviceRefused checks that a device outside the trust store is
// refused during the handshake and leaves no trace: no session, no peer and no
// event.
func TestUnknownDeviceRefused(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")

	if err := h.DialAsStranger(alpha); err == nil {
		t.Fatal("the unknown device was accepted")
	} else {
		t.Logf("the handshake was refused: %v", err)
	}

	alpha.NoEvent(node.EventPeerOnline, model.DeviceID{}, 300*time.Millisecond)
	if events := alpha.Events(); len(events) != 0 {
		t.Fatalf("the unknown device surfaced events: %s", alpha.summary())
	}

	peers, err := alpha.Peers(t.Context())
	if err != nil {
		t.Fatalf("list the peers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("the unknown device reached the peer list: %v", peers)
	}
}
