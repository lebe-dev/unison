package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// testTimeout bounds the waits of the tests.
const testTimeout = 60 * time.Second

// testVersion is the daemon version the tests report.
const testVersion = "1.2.3"

// socketFactory binds virtual packet connections on consecutive addresses of
// one node. The first socket is the main listener.
type socketFactory struct {
	network *vnet.Network
	next    int

	mu sync.Mutex
}

// listenPacket binds the next virtual address.
func (f *socketFactory) listenPacket(_ context.Context, _ int) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(f.next))
	f.next++
	return f.network.ListenPacket(addr)
}

// newNetwork returns a virtual network and a discovery bus with their cleanup.
func newNetwork(t *testing.T) (*vnet.Network, *fake.Bus) {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	return network, fake.NewBus()
}

// testDevice is one node with the local API served over a socket.
type testDevice struct {
	*node.Node

	name      string
	clipboard *fake.Clipboard
	clock     *fake.Clock
	endpoint  string
}

// newDevice creates a device on the virtual network and serves its local API.
// The node does not start on its own.
func newDevice(t *testing.T, network *vnet.Network, bus *fake.Bus, base int, opts api.Options) *testDevice {
	t.Helper()

	name := "device-" + strconv.Itoa(base)
	clipboard := fake.NewClipboard()
	clock := fake.NewClock(time.Now())
	created, err := node.New(node.Config{
		DataDir:   t.TempDir(),
		Name:      name,
		Platform:  model.PlatformMacOS,
		Keystore:  fake.NewKeystore(),
		Clipboard: clipboard,
		Discovery: bus.Discovery(),
		Clock:     clock,
		Logger:    testLogger(),
		Limits: node.Limits{
			DialTimeout:       3 * time.Second,
			ReconnectDelay:    50 * time.Millisecond,
			ReconnectMaxDelay: 500 * time.Millisecond,
		},
		ListenPacket: (&socketFactory{network: network, next: base}).listenPacket,
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	if opts.Version == "" {
		opts.Version = testVersion
	}
	if opts.DeviceName == "" {
		opts.DeviceName = name
	}
	device := &testDevice{Node: created, name: name, clipboard: clipboard, clock: clock}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := device.Stop(ctx); err != nil {
			t.Errorf("stop node: %v", err)
		}
	})

	service, err := api.New(created, opts)
	if err != nil {
		t.Fatalf("create the local API: %v", err)
	}
	_, handler := service.Handler()

	endpoint := filepath.Join(shortDir(t), "unisond.sock")
	listener, err := localconn.Listen(endpoint)
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
			t.Error("the local API did not stop")
		}
	})

	device.endpoint = endpoint
	return device
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

// testLogger returns a logger that writes to stderr when the test is verbose.
func testLogger() *slog.Logger {
	if !testing.Verbose() {
		return nil
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// start starts the node.
func (d *testDevice) start(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("start node: %v", err)
	}
}

// pair stores the other device as a Personal peer.
func (d *testDevice) pair(t *testing.T, other *testDevice) {
	t.Helper()

	d.addPeer(t, other, other.name)
}

// addPeer stores the other device under a name.
func (d *testDevice) addPeer(t *testing.T, other *testDevice, name string) {
	t.Helper()

	err := d.AddPeer(context.Background(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     name,
		Platform: model.PlatformMacOS,
		Role:     model.RolePersonal,
	})
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
}

// emit puts plain text into the clipboard of the device and waits until the
// pipeline captures it. A clipboard without a watcher drops the snapshot, so the
// snapshot is emitted again when the first one is lost. The clock is advanced
// without another snapshot, because a new snapshot restarts the debounce.
func (d *testDevice) emit(t *testing.T, text string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		d.clipboard.Emit(ports.Snapshot{Content: ports.Content{
			Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
		}})

		retry := time.Now().Add(time.Second)
		for time.Now().Before(retry) {
			if _, ok := d.Clip().Last(); ok {
				return
			}
			d.clock.Advance(clip.DefaultDebounce)
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatal("the pipeline did not capture the clipboard content")
}

// runCommand runs one command line and returns its exit code and output.
func runCommand(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()

	var stdout, stderr safeBuffer
	code := run(context.Background(), args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// session is one running command line with a pipe on standard input.
type session struct {
	in     *io.PipeWriter
	stdout *safeBuffer
	stderr *safeBuffer
	done   chan int
}

// startSession runs one command line in the background.
func startSession(t *testing.T, args ...string) *session {
	t.Helper()

	reader, writer := io.Pipe()
	running := &session{
		in:     writer,
		stdout: &safeBuffer{},
		stderr: &safeBuffer{},
		done:   make(chan int, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { running.done <- run(ctx, args, reader, running.stdout, running.stderr) }()
	t.Cleanup(func() { _ = writer.Close() })
	return running
}

// answer writes one line to standard input.
func (s *session) answer(t *testing.T, line string) {
	t.Helper()

	if _, err := io.WriteString(s.in, line+"\n"); err != nil {
		t.Fatalf("write the answer: %v", err)
	}
}

// wait waits for the command to end.
func (s *session) wait(t *testing.T) int {
	t.Helper()

	select {
	case code := <-s.done:
		return code
	case <-time.After(testTimeout):
		t.Fatalf("the command did not end, stdout %q stderr %q", s.stdout.String(), s.stderr.String())
		return -1
	}
}

// waitOutput waits until the stream carries the text.
func (s *session) waitOutput(t *testing.T, stream *safeBuffer, text string) string {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		output := stream.String()
		if strings.Contains(output, text) {
			return output
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %q on the stream, stdout %q stderr %q", text, s.stdout.String(), s.stderr.String())
	return ""
}

// safeBuffer is a buffer that several goroutines write and read.
type safeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write appends p to the buffer.
func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns the content of the buffer.
func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeLocalClip records the content that reaches the clipboard pipeline.
type fakeLocalClip struct {
	mu      sync.Mutex
	content ports.Content
	err     error
}

// Submit records the content and returns an item for it.
func (f *fakeLocalClip) Submit(_ context.Context, content ports.Content) (model.ClipItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return model.ClipItem{}, f.err
	}
	f.content = content
	item := model.ClipItem{
		ID:        model.ClipID{7},
		Seq:       7,
		CreatedAt: time.Now(),
		Kind:      model.KindText,
		Source:    model.SourceCLI,
	}
	for _, representation := range content.Representations {
		item.Representations = append(item.Representations, model.Representation{
			MIME:   representation.MIME,
			Size:   uint64(len(representation.Data)),
			Inline: representation.Data,
		})
	}
	return item, nil
}

// submitted returns the content the pipeline received.
func (f *fakeLocalClip) submitted() ports.Content {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.content
}

// codeOf returns the short authentication code from the output of pair.
func codeOf(t *testing.T, output string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "code" {
			continue
		}
		code := fields[1] + fields[2]
		if len(code) != 6 {
			t.Fatalf("code %q is not six digits", code)
		}
		return code
	}
	t.Fatalf("no code in the output %q", output)
	return ""
}
