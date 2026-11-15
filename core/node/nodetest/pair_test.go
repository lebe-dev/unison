package nodetest

import (
	"context"
	"testing"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
)

// TestPairWritesTrust checks that pairing directly stores both devices as
// Personal peers and connects them.
func TestPairWritesTrust(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")

	h.Pair(alpha, beta)

	peer, err := alpha.Trust().Get(context.Background(), beta.DeviceID())
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if peer.Role != model.RolePersonal {
		t.Fatalf("the role is %s, want %s", peer.Role, model.RolePersonal)
	}

	state, err := alpha.Peer(context.Background(), beta.DeviceID())
	if err != nil {
		t.Fatalf("read the peer state: %v", err)
	}
	if !state.Online || state.Path != model.PathLAN {
		t.Fatalf("the peer is not online on the LAN path: %+v", state)
	}

	alpha.Submit("from alpha")
	beta.WaitText("from alpha")
}

// TestPairOverLossyNetwork checks that the devices pair and sync while the
// network drops packets.
func TestPairOverLossyNetwork(t *testing.T) {
	h := New(t, WithLoss(0.05))
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")

	h.Pair(alpha, beta)

	alpha.Submit("paired over a lossy network")
	beta.WaitText("paired over a lossy network")
}

// TestPairSAS checks the pairing exchange and the sync that follows it.
func TestPairSAS(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")

	h.PairSAS(alpha, beta)

	peers, err := alpha.Trust().List(context.Background(), trust.ListFilter{})
	if err != nil {
		t.Fatalf("list the peers: %v", err)
	}
	if len(peers) != 1 || peers[0].DeviceID() != beta.DeviceID() {
		t.Fatalf("the trust store holds %v, want only %s", peers, beta.DeviceID())
	}

	alpha.Emit("paired with alpha")
	beta.WaitText("paired with alpha")
	beta.Emit("paired with beta")
	alpha.WaitText("paired with beta")
}
