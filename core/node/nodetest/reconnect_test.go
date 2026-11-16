package nodetest

import (
	"bytes"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/node"
)

// pairWindow is how long the tests watch an established pair after another
// device changed its connections.
const pairWindow = 300 * time.Millisecond

// pairMark is the state of one pair at the moment it settled.
type pairMark struct {
	peer     *Node
	events   int
	sessions int
}

// TestThirdPairLeavesPairConnected checks that pairing a third device does not
// replace the connection of a pair that is already online.
func TestThirdPairLeavesPairConnected(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	gamma := h.NewNode("gamma")

	h.Pair(alpha, beta)
	alpha.WaitOnline(beta)
	beta.WaitOnline(alpha)

	// The device with the lower ID holds the connection both devices settle on,
	// so the pair is watched while that device pairs again.
	pairer, other := lowerFirst(alpha, beta)
	markPairer := h.markPair(pairer, other)
	markOther := h.markPair(other, pairer)

	h.Pair(pairer, gamma)
	pairer.WaitOnline(gamma)
	gamma.WaitOnline(pairer)

	time.Sleep(pairWindow)
	checkPair(t, pairer, markPairer)
	checkPair(t, other, markOther)
}

// TestHubKeepsPairsConnected pairs one device with a row of others and checks
// that every pair it already had keeps its connection.
func TestHubKeepsPairsConnected(t *testing.T) {
	h := New(t)
	hub, peers := h.hubWithPeers(4)

	marks := make([]pairMark, 0, len(peers))
	for _, peer := range peers {
		h.Pair(hub, peer)
		hub.WaitOnline(peer)
		peer.WaitOnline(hub)
		marks = append(marks, h.markPair(hub, peer))
	}

	time.Sleep(pairWindow)
	for _, mark := range marks {
		checkPair(t, hub, mark)
	}
}

// hubWithPeers returns the node with the lowest device ID of the harness and
// count other devices. Only the lower device of a pair holds the connection both
// devices settle on, so the hub keeps its pairs while it pairs again.
func (h *Harness) hubWithPeers(count int) (*Node, []*Node) {
	h.t.Helper()

	nodes := make([]*Node, 0, count+1)
	for i := range count + 1 {
		nodes = append(nodes, h.NewNode("device-"+string(rune('a'+i))))
	}
	hub := nodes[0]
	for _, n := range nodes[1:] {
		if bytes.Compare(n.DeviceID().Bytes(), hub.DeviceID().Bytes()) < 0 {
			hub = n
		}
	}
	peers := make([]*Node, 0, count)
	for _, n := range nodes {
		if n != hub {
			peers = append(peers, n)
		}
	}
	return hub, peers
}

// lowerFirst returns the two devices, the one with the lower device ID first.
func lowerFirst(one, other *Node) (*Node, *Node) {
	if bytes.Compare(other.DeviceID().Bytes(), one.DeviceID().Bytes()) < 0 {
		return other, one
	}
	return one, other
}

// markPair records the state of the pair, which the checks compare with.
func (h *Harness) markPair(one, other *Node) pairMark {
	h.t.Helper()
	return pairMark{
		peer:     other,
		events:   one.eventCount(),
		sessions: one.SessionCount(other),
	}
}

// checkPair checks that the connection of the pair was neither replaced nor
// reported as lost since the mark. One online event is allowed, as the mark may
// sit before the event of the session that was open.
func checkPair(t *testing.T, one *Node, mark pairMark) {
	t.Helper()

	if got := one.SessionCount(mark.peer); got != mark.sessions {
		t.Fatalf("node %s replaced its connection with %s: %d sessions, want %d (sessions: %s)",
			one.name, mark.peer.name, got, mark.sessions, one.sessions)
	}
	online, offline := countPeerEvents(one, mark.peer, mark.events)
	if offline > 0 {
		t.Fatalf("node %s reported %s offline %d times, want none (events: %s)",
			one.name, mark.peer.name, offline, one.summary())
	}
	if online > 1 {
		t.Fatalf("node %s reported %s online %d times, want at most one (events: %s)",
			one.name, mark.peer.name, online, one.summary())
	}
}

// countPeerEvents counts the peer online and offline events of the node after
// the first skip events.
func countPeerEvents(n *Node, peer *Node, skip int) (online, offline int) {
	events := n.Events()
	if skip > len(events) {
		skip = len(events)
	}
	for _, event := range events[skip:] {
		if event.Peer != peer.DeviceID() {
			continue
		}
		switch event.Kind {
		case node.EventPeerOnline:
			online++
		case node.EventPeerOffline:
			offline++
		}
	}
	return online, offline
}
