package nodetest

import (
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/node"
)

// TestTextBothWays moves text from one device to the other and back.
func TestTextBothWays(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	alpha.Emit("hello from alpha")
	beta.WaitText("hello from alpha")

	event := beta.WaitEvent(node.EventClipboard, alpha.DeviceID())
	if event.Local {
		t.Fatal("an incoming item is marked as local")
	}
	if event.Item.Origin != alpha.DeviceID() {
		t.Fatalf("the origin is %s, want %s", event.Item.Origin, alpha.DeviceID())
	}
	if text, ok := textOf(event.Item); !ok || text != "hello from alpha" {
		t.Fatalf("the event carries %q (set %v)", text, ok)
	}

	beta.Emit("hello from beta")
	alpha.WaitText("hello from beta")
}

// TestOfflineReceivesOnlyNewText takes a device off the network, copies text
// while it is away and checks that it gets only the text copied after it comes
// back. History of the missed items is out of scope.
func TestOfflineReceivesOnlyNewText(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	h.Offline(beta, alpha)

	alpha.Submit("copied while beta was away")

	h.Online(beta, alpha)

	alpha.Submit("copied after beta came back")
	beta.WaitText("copied after beta came back")
	beta.NoClipboardText("copied while beta was away", 500*time.Millisecond)

	if text, ok := beta.Text(); !ok || text != "copied after beta came back" {
		t.Fatalf("the clipboard holds %q (set %v) after the quiet window", text, ok)
	}
}

// TestAddressChangeReconnects moves a device to another address and checks that
// the peers meet again and keep syncing.
func TestAddressChangeReconnects(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	alpha.Emit("before the move")
	beta.WaitText("before the move")

	previous := beta.ListenAddr()
	h.ChangeAddress(beta, alpha)
	if beta.ListenAddr() == previous {
		t.Fatalf("the device kept its address %s", previous)
	}

	beta.Emit("after the move")
	alpha.WaitText("after the move")
}

// TestLossyLink checks that a five percent loss does not break the sync.
func TestLossyLink(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	h.SetLoss(0.05)
	alpha.Emit("across a lossy link")
	beta.WaitText("across a lossy link")

	beta.Emit("and back across it")
	alpha.WaitText("and back across it")
}

// TestLatentLink checks that a delayed link keeps the sync working.
func TestLatentLink(t *testing.T) {
	h := New(t, WithLatency(25*time.Millisecond))
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	alpha.Emit("across a slow link")
	beta.WaitText("across a slow link")
}

// TestThreeNodesShareText checks that every device of a mesh receives the text
// of another one.
func TestThreeNodesShareText(t *testing.T) {
	h := New(t)

	names := []string{"alpha", "beta", "gamma"}
	nodes := make([]*Node, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, h.NewNode(name))
	}
	for i, one := range nodes {
		for _, other := range nodes[i+1:] {
			h.Pair(one, other)
		}
	}
	h.Settle()

	nodes[2].Emit("to the whole room")
	nodes[0].WaitText("to the whole room")
	nodes[1].WaitText("to the whole room")

	if len(h.Nodes()) != len(names) {
		t.Fatalf("the harness runs %d nodes, want %d", len(h.Nodes()), len(names))
	}
}
