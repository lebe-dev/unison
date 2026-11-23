package nodetest

import (
	"context"
	"testing"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
)

// TestPeerPlatformFromHello checks that a device on Linux connects to a device
// of this version and that the peer stores the platform it announced.
func TestPeerPlatformFromHello(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNodeWithPlatform("beta", model.PlatformLinux)

	// The trust records start without a platform, so the handshake fills it in.
	for _, pair := range [][2]*Node{{alpha, beta}, {beta, alpha}} {
		from, to := pair[0], pair[1]
		peer := trust.Peer{Public: to.Identity().Public(), Name: to.name, Role: model.RolePersonal}
		if err := from.AddPeer(context.Background(), peer); err != nil {
			t.Fatalf("node %s: add peer %s: %v", from.name, to.name, err)
		}
	}
	alpha.WaitOnline(beta)
	beta.WaitOnline(alpha)

	peer, err := alpha.Trust().Get(context.Background(), beta.DeviceID())
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if peer.Platform != model.PlatformLinux {
		t.Fatalf("peer platform = %s, want %s", peer.Platform, model.PlatformLinux)
	}

	peer, err = beta.Trust().Get(context.Background(), alpha.DeviceID())
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if peer.Platform != model.PlatformMacOS {
		t.Fatalf("peer platform = %s, want %s", peer.Platform, model.PlatformMacOS)
	}
}
