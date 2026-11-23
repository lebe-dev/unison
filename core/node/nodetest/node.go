package nodetest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/session"
)

// settleDelay is how long a session has to stay in place before a test uses it.
// It exceeds the reconnect backoff of the harness, so a connection that the
// devices are still replacing shows up as a new session within the wait.
const settleDelay = 500 * time.Millisecond

// Node is one device of a Harness. It embeds the node, so the tests reach the
// trust store, the clipboard pipeline and the rest of the public API directly.
type Node struct {
	*node.Node

	harness   *Harness
	name      string
	platform  model.Platform
	clipboard *fake.Clipboard
	sockets   *sockets
	sessions  *sessionLog
	sub       *node.Subscriber

	mu     sync.Mutex
	events []node.Event
	notify chan struct{}
	done   chan struct{}
}

// Name returns the device name.
func (n *Node) Name() string {
	return n.name
}

// ListenAddr returns the address of the main listener.
func (n *Node) ListenAddr() string {
	return n.sockets.listenAddr()
}

// Clipboard returns the fake clipboard of the device.
func (n *Node) Clipboard() *fake.Clipboard {
	return n.clipboard
}

// Text returns the plain text last written to the clipboard.
func (n *Node) Text() (string, bool) {
	content, ok := n.clipboard.LastWrite()
	if !ok {
		return "", false
	}
	for _, representation := range content.Representations {
		if representation.MIME == clip.MIMETextPlain {
			return string(representation.Data), true
		}
	}
	return "", false
}

// Emit puts text on the clipboard of the device, as if the user copied it. The
// pipeline merges the change and announces the item.
func (n *Node) Emit(text string) {
	n.clipboard.Emit(ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
	}})
}

// Submit sends text through the local path of the pipeline, which skips the
// merge window of a captured change.
func (n *Node) Submit(text string) model.ClipItem {
	n.harness.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), n.harness.timeout)
	defer cancel()
	item, err := n.Clip().Submit(ctx, ports.Content{
		Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
	})
	if err != nil {
		n.harness.t.Fatalf("node %s: submit text: %v", n.name, err)
	}
	return item
}

// WaitText waits until the clipboard holds the plain text want.
func (n *Node) WaitText(want string) {
	n.harness.t.Helper()

	deadline := time.Now().Add(n.harness.timeout)
	for {
		if text, ok := n.Text(); ok && text == want {
			return
		}
		if time.Now().After(deadline) {
			text, ok := n.Text()
			n.harness.t.Fatalf("node %s: clipboard holds %q (set %v) within %v, want %q (events: %s)",
				n.name, text, ok, n.harness.timeout, want, n.summary())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Events returns the events the node published so far.
func (n *Node) Events() []node.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]node.Event(nil), n.events...)
}

// WaitEvent waits until the node published an event of the kind about the peer.
// A zero peer matches any device. An event published before the call matches
// too.
func (n *Node) WaitEvent(kind node.EventKind, peer model.DeviceID) node.Event {
	n.harness.t.Helper()

	event, ok := n.waitEvent(kind, peer, n.harness.timeout, 0)
	if !ok {
		n.harness.t.Fatalf("node %s: no %s event for %s within %v (events: %s)",
			n.name, kind, peerLabel(peer), n.harness.timeout, n.summary())
	}
	return event
}

// WaitIncoming waits until the peer announced an item that carries text.
func (n *Node) WaitIncoming(peer *Node, text string) node.Event {
	n.harness.t.Helper()

	deadline := time.Now().Add(n.harness.timeout)
	for {
		for _, event := range n.Events() {
			if event.Kind != node.EventClipboard || event.Local || event.Peer != peer.DeviceID() {
				continue
			}
			if got, ok := textOf(event.Item); ok && got == text {
				return event
			}
		}
		if time.Now().After(deadline) {
			n.harness.t.Fatalf("node %s: no item with %q from %s within %v (events: %s; sessions: %s)",
				n.name, text, peer.name, n.harness.timeout, n.summary(), n.sessions)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// NoEvent checks that the node publishes no event of the kind about the peer
// during quiet.
func (n *Node) NoEvent(kind node.EventKind, peer model.DeviceID, quiet time.Duration) {
	n.harness.t.Helper()

	skip := n.eventCount()
	if event, ok := n.waitEvent(kind, peer, quiet, skip); ok {
		n.harness.t.Fatalf("node %s: unexpected %s event for %s: %+v",
			n.name, kind, peerLabel(event.Peer), event)
	}
}

// NoClipboardText checks that text neither arrives on the clipboard nor is
// announced by a peer during quiet.
func (n *Node) NoClipboardText(text string, quiet time.Duration) {
	n.harness.t.Helper()

	skip := n.eventCount()
	deadline := time.Now().Add(quiet)
	for time.Now().Before(deadline) {
		if current, ok := n.Text(); ok && current == text {
			n.harness.t.Fatalf("node %s: the clipboard received %q within %v", n.name, text, quiet)
		}

		n.mu.Lock()
		recent := append([]node.Event(nil), n.events[skip:]...)
		n.mu.Unlock()
		for _, event := range recent {
			if event.Kind != node.EventClipboard || event.Local {
				continue
			}
			if got, ok := textOf(event.Item); ok && got == text {
				n.harness.t.Fatalf("node %s: the peer %s announced %q within %v",
					n.name, peerLabel(event.Peer), text, quiet)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// WaitOnline waits until the node holds a session with peer and the session has
// settled, which is when the devices stopped replacing each other's
// connections.
func (n *Node) WaitOnline(peer *Node) {
	n.harness.t.Helper()

	n.waitState(peer, true)
	n.waitSettled(peer)
}

// WaitOffline waits until the node holds no session with peer. A device that
// lost the network notices the broken link on its own, so a test that takes a
// device offline waits for both sides before going on.
func (n *Node) WaitOffline(peer *Node) {
	n.waitState(peer, false)
}

// SessionCount returns how many sessions the node opened with peer so far. A
// count that grows while the peer stays online means the connection was
// replaced.
func (n *Node) SessionCount(peer *Node) int {
	return n.sessions.count(peer.DeviceID())
}

// waitState waits until the node reports the peer as online or offline.
func (n *Node) waitState(peer *Node, online bool) {
	n.harness.t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(n.harness.timeout)
	want := "offline"
	if online {
		want = "online"
	}
	var state model.PeerState
	var err error
	for {
		state, err = n.Peer(ctx, peer.DeviceID())
		if err == nil && state.Online == online {
			return
		}
		if time.Now().After(deadline) {
			n.harness.t.Fatalf("node %s: the peer %s did not go %s within %v (online %v, error %v)",
				n.name, peer.name, want, n.harness.timeout, state.Online, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitSettled waits until the newest session with the peer stays in place for
// the settle delay.
func (n *Node) waitSettled(peer *Node) {
	n.harness.t.Helper()

	deadline := time.Now().Add(n.harness.timeout)
	for {
		if n.sessions.settled(peer.DeviceID(), settleDelay) {
			return
		}
		if time.Now().After(deadline) {
			n.harness.t.Fatalf("node %s: the session with %s did not settle within %v (events: %s; sessions: %s)",
				n.name, peer.name, n.harness.timeout, n.summary(), n.sessions)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// trackSession records a new session for the waits.
func (n *Node) trackSession(_ context.Context, sess *session.Session) error {
	n.sessions.add(sess)
	return nil
}

// Offline takes the device off the network: the packets of every socket are
// dropped until Online. The device keeps its addresses.
func (n *Node) Offline() {
	n.NetworkChanged(model.NetworkState{Online: false})
	n.sockets.setDown(true)
}

// Online returns the device to the network and reports the network change, so
// the node announces itself and retries the peers.
func (n *Node) Online() {
	n.sockets.setDown(false)
	n.NetworkChanged(model.NetworkState{Online: true})
}

// MoveAddress binds every socket of the device to a new address, as a device
// that joined another network does.
func (n *Node) MoveAddress() {
	n.harness.t.Helper()

	previous, err := n.sockets.moveAll()
	if err != nil {
		n.harness.t.Fatalf("node %s: change the address: %v", n.name, err)
	}
	n.harness.t.Logf("node %s: moved from %v to %s", n.name, previous, n.ListenAddr())
}

// close stops the node and the event recorder.
func (n *Node) close() {
	ctx, cancel := context.WithTimeout(context.Background(), n.harness.timeout)
	defer cancel()
	if err := n.Stop(ctx); err != nil {
		n.harness.t.Errorf("node %s: stop: %v", n.name, err)
	}
	n.sub.Close()
	<-n.done
}

// record collects the events of the node for the waiters.
func (n *Node) record() {
	defer close(n.done)

	for event := range n.sub.Events() {
		n.mu.Lock()
		n.events = append(n.events, event)
		close(n.notify)
		n.notify = make(chan struct{})
		n.mu.Unlock()
	}
}

// eventCount returns the number of events collected so far.
func (n *Node) eventCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.events)
}

// waitEvent waits until the node published a matching event after the first
// skip events.
func (n *Node) waitEvent(kind node.EventKind, peer model.DeviceID, timeout time.Duration, skip int) (node.Event, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		n.mu.Lock()
		for _, event := range n.events[skip:] {
			if event.Kind == kind && (peer.IsZero() || event.Peer == peer) {
				n.mu.Unlock()
				return event, true
			}
		}
		notify := n.notify
		n.mu.Unlock()

		select {
		case <-notify:
		case <-timer.C:
			return node.Event{}, false
		}
	}
}

// summary describes the collected events, which makes a failed wait easier to
// read.
func (n *Node) summary() string {
	events := n.Events()
	if len(events) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(events))
	for _, event := range events {
		parts = append(parts, event.Kind.String()+"/"+peerLabel(event.Peer))
	}
	return strings.Join(parts, ", ")
}

// peerLabel returns a short form of the device ID.
func peerLabel(id model.DeviceID) string {
	if id.IsZero() {
		return "any device"
	}
	return id.String()[:8]
}

// textOf returns the plain text of an item.
func textOf(item model.ClipItem) (string, bool) {
	for _, representation := range item.Representations {
		if representation.MIME != clip.MIMETextPlain {
			continue
		}
		return string(representation.Inline), true
	}
	return "", false
}

// String returns the device name and its ID.
func (n *Node) String() string {
	return fmt.Sprintf("%s (%s)", n.name, n.DeviceID())
}
