package node

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// clipID returns a clip ID built from one byte.
func clipID(b byte) model.ClipID {
	var id model.ClipID
	copy(id[:], bytes.Repeat([]byte{b}, len(id)))
	return id
}

// historyTexts returns the plain texts of the live entries of the node.
func historyTexts(t *testing.T, n *testNode) []string {
	t.Helper()

	items, err := n.History().List(context.Background(), history.ListFilter{Limit: history.MaxListLimit})
	if err != nil {
		t.Fatalf("list the history of the node: %v", err)
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		for _, rep := range item.Representations {
			if rep.MIME == clip.MIMETextPlain {
				texts = append(texts, string(rep.Inline))
			}
		}
	}
	return texts
}

// waitHistoryText waits until the history of the node holds the text.
func waitHistoryText(t *testing.T, n *testNode, text string) model.ClipID {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		items, err := n.History().List(context.Background(), history.ListFilter{Limit: history.MaxListLimit})
		if err != nil {
			t.Fatalf("list the history of the node: %v", err)
		}
		for _, item := range items {
			for _, rep := range item.Representations {
				if rep.MIME == clip.MIMETextPlain && string(rep.Inline) == text {
					return item.ID
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the history did not receive %q within %v, it holds %v", text, testTimeout, historyTexts(t, n))
	return model.ClipID{}
}

// stopNode stops the node at once.
func stopNode(t *testing.T, n *testNode) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("stop node: %v", err)
	}
}

// waitHistoryGone waits until the item is absent from the history of the node.
func waitHistoryGone(t *testing.T, n *testNode, id model.ClipID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if _, err := n.History().Get(context.Background(), id); errors.Is(err, history.ErrNotFound) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the item %s is still in the history of %s, it holds %v", id, n.DeviceID(), historyTexts(t, n))
}

// waitHistoryItem waits until the item is in the history of the node.
func waitHistoryItem(t *testing.T, n *testNode, id model.ClipID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if _, err := n.History().Get(context.Background(), id); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the item %s did not reach %s within %v", id, n.DeviceID(), testTimeout)
}

// resync drops the sessions of from and waits until the other devices opened a
// new one, which starts the history exchange again.
func resync(t *testing.T, from *testNode, others ...*testNode) {
	t.Helper()

	before := make([]uint64, len(others))
	for i, other := range others {
		before[i] = other.generation(from.DeviceID())
	}
	from.closeSessions()
	for i, other := range others {
		waitNewSession(t, other, from.DeviceID(), before[i])
	}
}

// waitMeshOnline waits until every node has a session with every other node.
func waitMeshOnline(t *testing.T, nodes ...*testNode) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		online := true
		for _, n := range nodes {
			for _, other := range nodes {
				if n != other && !n.online(other.DeviceID()) {
					online = false
				}
			}
		}
		if online {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, n := range nodes {
		for _, other := range nodes {
			if n != other {
				t.Logf("%s -> %s: %s", n.DeviceID(), other.DeviceID(), n.linkState(other.DeviceID()))
			}
		}
	}
	t.Fatal("the devices did not open sessions with each other")
}

// mesh pairs every device with every other device.
func mesh(t *testing.T, nodes ...*testNode) {
	t.Helper()

	for _, from := range nodes {
		for _, to := range nodes {
			if from == to {
				continue
			}
			from.pair(t, to)
		}
	}
	for _, from := range nodes {
		for _, to := range nodes {
			if from == to {
				continue
			}
			waitOnline(t, from, to.DeviceID())
		}
	}
}

// TestHistoryConvergesAfterOffline checks that three devices end with the same
// history after one of them was offline, and that a deletion reaches them all.
func TestHistoryConvergesAfterOffline(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	dir := t.TempDir()
	keys := fake.NewKeystore()

	a := newTestNode(t, network, bus, 30000)
	b := newTestNode(t, network, bus, 31000)
	c := newTestNodeAt(t, network, bus, 32000, dir, keys)
	a.start(t)
	b.start(t)
	c.start(t)
	mesh(t, a, b, c)

	a.clipboard.Emit(textSnapshot("one"))
	waitHistoryText(t, b, "one")
	waitHistoryText(t, c, "one")

	// The third device goes offline with the history it has.
	stopNode(t, c)

	a.clipboard.Emit(textSnapshot("two"))
	waitHistoryText(t, b, "two")
	b.clipboard.Emit(textSnapshot("three"))
	waitHistoryText(t, a, "three")

	// The device returns with its data and catches up with the entries it
	// missed while it was away.
	c = newTestNodeAt(t, network, bus, 32000, dir, keys)
	c.start(t)
	waitMeshOnline(t, a, b, c)
	waitHistoryText(t, c, "two")
	waitHistoryText(t, c, "three")
	waitHistoryText(t, b, "three")

	// A deletion of one device reaches the others with the next exchange.
	id := waitHistoryText(t, a, "two")
	if err := a.History().Delete(context.Background(), id); err != nil {
		t.Fatalf("delete the item: %v", err)
	}
	waitHistoryGone(t, a, id)

	resync(t, a, b, c)
	waitHistoryGone(t, b, id)
	waitHistoryGone(t, c, id)
}

// TestHistoryChangeReachesSubscribers checks that an entry the history exchange
// records is reported to the subscribers like a captured one: a reader that
// only listens to events would otherwise keep a list that misses it.
func TestHistoryChangeReachesSubscribers(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	dir := t.TempDir()
	keys := fake.NewKeystore()

	a := newTestNode(t, network, bus, 33000)
	b := newTestNodeAt(t, network, bus, 33100, dir, keys)
	a.start(t)
	b.start(t)
	mesh(t, a, b)

	a.clipboard.Emit(textSnapshot("before"))
	waitHistoryText(t, b, "before")

	// The device is away while the other one captures an entry.
	stopNode(t, b)
	a.clipboard.Emit(textSnapshot("after"))
	waitHistoryText(t, a, "after")

	// The returning device learns the entry from the exchange.
	b = newTestNodeAt(t, network, bus, 33100, dir, keys)
	b.start(t)
	sub := b.Subscribe()
	defer sub.Close()

	waitMeshOnline(t, a, b)
	waitHistoryText(t, b, "after")
	waitEvent(t, sub, EventHistory, model.DeviceID{})
}

// TestHistoryExpiryWithoutNetwork checks that an entry that reaches its expiry
// disappears from every device while the network is down.
func TestHistoryExpiryWithoutNetwork(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 33000)
	b := newTestNode(t, network, bus, 34000)
	c := newTestNode(t, network, bus, 35000)
	a.start(t)
	b.start(t)
	c.start(t)
	mesh(t, a, b, c)

	created := time.Now()
	expires := created.Add(5 * time.Second)
	item := model.ClipItem{
		ID:        clipID(42),
		Origin:    a.DeviceID(),
		Seq:       1000,
		CreatedAt: created,
		ExpiresAt: expires,
		Kind:      model.KindText,
		Source:    model.SourceCLI,
		Representations: []model.Representation{{
			MIME:   clip.MIMETextPlain,
			Size:   5,
			Hash:   blake3.Sum256([]byte("short")),
			Inline: []byte("short"),
		}},
	}
	if err := a.History().Append(context.Background(), item); err != nil {
		t.Fatalf("append the item: %v", err)
	}

	resync(t, a, b, c)
	waitHistoryItem(t, b, item.ID)
	waitHistoryItem(t, c, item.ID)
	if _, err := a.History().Get(context.Background(), item.ID); err != nil {
		t.Fatalf("the item is absent on the origin device: %v", err)
	}

	// No device can reach another one while the entry expires.
	for _, n := range []*testNode{a, b, c} {
		n.sockets.disconnect()
	}
	time.Sleep(time.Until(expires) + 300*time.Millisecond)

	for _, n := range []*testNode{a, b, c} {
		if _, err := n.History().Get(context.Background(), item.ID); !errors.Is(err, history.ErrNotFound) {
			t.Fatalf("the expired item is still on %s: %v", n.DeviceID(), err)
		}
	}
	for _, n := range []*testNode{a, b, c} {
		n.sockets.reconnect()
	}
}
