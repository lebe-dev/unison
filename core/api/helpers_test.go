package api_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// testTimeout bounds the waits of the tests.
const testTimeout = 30 * time.Second

// maxTestPort is the highest port of the virtual network that a node can dial.
const maxTestPort = 65535

// socketFactory binds virtual packet connections on consecutive addresses of
// one node. The first socket is the main listener.
type socketFactory struct {
	network *vnet.Network
	next    int

	mu sync.Mutex
}

// newSocketFactory returns a factory that starts at base.
func newSocketFactory(network *vnet.Network, base int) *socketFactory {
	return &socketFactory{network: network, next: base}
}

// listenPacket binds the next virtual address.
func (f *socketFactory) listenPacket(_ context.Context, _ int) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.next > maxTestPort {
		return nil, fmt.Errorf("socket factory: port %d of the virtual network is out of range", f.next)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(f.next))
	f.next++
	return f.network.ListenPacket(addr)
}

// shortDir returns a temporary directory with a short path, so a socket path
// stays below the limit of the platform.
func shortDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "unison")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// testNode is one device on the virtual network.
type testNode struct {
	*node.Node
	name      string
	clipboard *fake.Clipboard
	clock     *fake.Clock
}

// newNode creates a node on the virtual network. The node does not start on its
// own.
func newNode(t *testing.T, network *vnet.Network, bus *fake.Bus, base int) *testNode {
	t.Helper()

	return newNodeIn(t, network, bus, base, t.TempDir(), fake.NewKeystore())
}

// newNodeIn creates a node in the given data directory and keystore, so a test
// can create the device again with the same identity and history.
func newNodeIn(t *testing.T, network *vnet.Network, bus *fake.Bus, base int, dataDir string, keystore ports.Keystore) *testNode {
	t.Helper()

	name := fmt.Sprintf("device-%d", base)
	clipboard := fake.NewClipboard()
	clock := fake.NewClock(time.Now())
	created, err := node.New(node.Config{
		DataDir:        dataDir,
		Name:           name,
		Platform:       model.PlatformMacOS,
		Keystore:       keystore,
		Clipboard:      clipboard,
		Discovery:      bus.Discovery(),
		Clock:          clock,
		Logger:         testLogger(),
		ShareDownloads: filepath.Join(t.TempDir(), "Downloads"),
		Limits: node.Limits{
			DialTimeout:       3 * time.Second,
			ReconnectDelay:    50 * time.Millisecond,
			ReconnectMaxDelay: 500 * time.Millisecond,
		},
		ListenPacket: newSocketFactory(network, base).listenPacket,
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	n := &testNode{Node: created, name: name, clipboard: clipboard, clock: clock}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := n.Stop(ctx); err != nil {
			t.Errorf("stop node: %v", err)
		}
	})
	return n
}

// enableFriends turns the friends discovery mode on, so Friends devices may open
// sessions with each other.
func enableFriends(t *testing.T, nodes ...*testNode) {
	t.Helper()

	for _, n := range nodes {
		if _, err := n.SetFriendsDiscovery(t.Context(), true); err != nil {
			t.Fatalf("enable friends discovery: %v", err)
		}
	}
}

// testLogger returns a logger that writes to stderr when the test is verbose.
func testLogger() *slog.Logger {
	if !testing.Verbose() {
		return nil
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// start starts the node.
func (n *testNode) start(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("start node: %v", err)
	}
}

// pair stores the other node as a Personal peer.
func (n *testNode) pair(t *testing.T, other *testNode) {
	t.Helper()

	err := n.AddPeer(context.Background(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     other.name,
		Platform: model.PlatformMacOS,
		Role:     model.RolePersonal,
	})
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
}

// newClient serves the local API of the node over a socket and returns a client
// of it.
func newClient(t *testing.T, n *node.Node, opts api.Options) localv1connect.UnisonServiceClient {
	t.Helper()

	service, err := api.New(n, opts)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	_, handler := service.Handler()

	path := filepath.Join(shortDir(t), "unison.sock")
	listener, err := localconn.Listen(path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- localconn.Serve(ctx, listener, handler) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(testTimeout):
			t.Error("serve did not return")
		}
	})

	return localconn.NewClient(path)
}

// callTimeout returns a context bounded by the test timeout.
func callTimeout(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// emitClipboard puts plain text into the clipboard of the node and advances the
// fake clock so the pipeline captures it.
func emitClipboard(t *testing.T, n *testNode, text string) {
	t.Helper()

	n.clipboard.Emit(ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
	}})

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if _, ok := n.Clip().Last(); ok {
			return
		}
		n.clock.Advance(clip.DefaultDebounce)
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the pipeline did not capture the clipboard content")
}

// textOf returns the plain text of an item.
func textOf(t *testing.T, item *localv1.ClipItem) string {
	t.Helper()

	for _, representation := range item.GetRepresentations() {
		if representation.GetMimeType() == clip.MIMETextPlain {
			return string(representation.GetInlineBytes())
		}
	}
	t.Fatal("the item has no plain text")
	return ""
}

// requireCode fails the test unless err carries the code.
func requireCode(t *testing.T, err error, code connect.Code) {
	t.Helper()

	if err == nil {
		t.Fatalf("call succeeded, want error %s", code)
	}
	if got := connect.CodeOf(err); got != code {
		t.Fatalf("call failed with %s (%v), want %s", got, err, code)
	}
}

// streamEvent is one item of the event stream: an event or the error that ended
// the stream.
type streamEvent struct {
	event *localv1.Event
	err   error
}

// watch opens the event stream and returns its items through a channel. The
// stream is opened in the background, because the response of a server stream
// arrives with the first event.
func watch(t *testing.T, client localv1connect.UnisonServiceClient) <-chan streamEvent {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	items := make(chan streamEvent)
	go func() {
		defer close(items)

		stream, err := client.WatchEvents(ctx, connect.NewRequest(&localv1.WatchEventsRequest{}))
		if err != nil {
			items <- streamEvent{err: err}
			return
		}
		defer func() { _ = stream.Close() }()

		for stream.Receive() {
			select {
			case items <- streamEvent{event: stream.Msg().GetEvent()}:
			case <-ctx.Done():
				return
			}
		}
		if err := stream.Err(); err != nil {
			items <- streamEvent{err: err}
		}
	}()
	return items
}

// waitEvent waits for an event of the kind.
func waitEvent(t *testing.T, items <-chan streamEvent, kind localv1.EventKind) *localv1.Event {
	t.Helper()

	timeout := time.After(testTimeout)
	for {
		select {
		case item, ok := <-items:
			if !ok {
				t.Fatalf("event stream closed while waiting for %s", kind)
			}
			if item.err != nil {
				t.Fatalf("event stream failed: %v", item.err)
			}
			if item.event.GetKind() == kind {
				return item.event
			}
		case <-timeout:
			t.Fatalf("no %s event within %v", kind, testTimeout)
		}
	}
}
