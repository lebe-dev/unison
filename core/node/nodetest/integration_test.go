//go:build integration

package nodetest

import (
	"fmt"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/node"
)

// announceGap keeps a stream of items under the announcement rate limit of a
// session.
const announceGap = 250 * time.Millisecond

// TestLossySoak sends a sequence of items over a lossy and delayed link and
// checks that every item arrives in the order it was sent.
func TestLossySoak(t *testing.T) {
	h := New(t, WithLatency(20*time.Millisecond), WithTimeout(60*time.Second))
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	h.SetSeed(1)
	h.SetLoss(0.05)

	// Announcements are rate limited per session, so the items are paced.
	const count = 20
	want := make([]string, 0, count)
	for i := range count {
		text := fmt.Sprintf("item %d over a lossy link", i)
		want = append(want, text)
		alpha.Submit(text)
		time.Sleep(announceGap)
	}
	beta.WaitIncoming(alpha, want[count-1])

	got := incomingTexts(beta, alpha)
	if len(got) != len(want) {
		t.Fatalf("the device received %d items, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// incomingTexts returns the texts the peer announced to the device, in the order
// the device received them.
func incomingTexts(n, peer *Node) []string {
	texts := make([]string, 0)
	for _, event := range n.Events() {
		if event.Kind != node.EventClipboard || event.Local || event.Peer != peer.DeviceID() {
			continue
		}
		if text, ok := textOf(event.Item); ok {
			texts = append(texts, text)
		}
	}
	return texts
}

// TestMeshUnderLoad lets every device of a mesh send to all the others over a
// lossy and delayed link.
func TestMeshUnderLoad(t *testing.T) {
	h := New(t, WithLatency(10*time.Millisecond), WithTimeout(60*time.Second))

	names := []string{"alpha", "beta", "gamma", "delta"}
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

	h.SetSeed(2)
	h.SetLoss(0.05)

	const rounds = 5
	for round := range rounds {
		for i, sender := range nodes {
			sender.Submit(fmt.Sprintf("round %d from device %d", round, i))
		}
	}

	last := fmt.Sprintf("round %d from device ", rounds-1)
	for i, receiver := range nodes {
		for j, sender := range nodes {
			if i == j {
				continue
			}
			receiver.WaitIncoming(sender, last+fmt.Sprint(j))
		}
	}
}

// TestAddressChangeUnderLoad moves one device to another address and checks
// that the peers meet again and keep syncing.
func TestAddressChangeUnderLoad(t *testing.T) {
	h := New(t, WithTimeout(60*time.Second))
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	alpha.Submit("before the move")
	beta.WaitIncoming(alpha, "before the move")

	h.ChangeAddress(beta, alpha)

	beta.Submit("after the move")
	alpha.WaitIncoming(beta, "after the move")
}
