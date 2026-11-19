package daemon_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/internal/daemon"
	"github.com/lebe-dev/unison/internal/journal"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// waitTimeout bounds the waits of the tests.
const waitTimeout = 30 * time.Second

// shortDir returns a temporary directory with a short path, so the endpoint of
// the daemon stays below the path limit of the platform.
func shortDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "unisond")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// testStderr returns the writer the daemon copies its log to, which is stderr
// when the test is verbose.
func testStderr() io.Writer {
	if !testing.Verbose() {
		return nil
	}
	return os.Stderr
}

// adapters are the platform ports of a test daemon. A restart on the same data
// directory reuses them, like a real device keeps its Keychain.
type adapters struct {
	keystore  *fake.Keystore
	clipboard *fake.Clipboard
	bus       *fake.Bus
}

// newAdapters returns platform ports for a fresh data directory.
func newAdapters() *adapters {
	return &adapters{keystore: fake.NewKeystore(), clipboard: fake.NewClipboard(), bus: fake.NewBus()}
}

// instance is a running daemon.
type instance struct {
	endpoint string
	cancel   context.CancelFunc
	done     chan error
	err      error
	stopped  bool
}

// start runs a daemon on dataDir and waits until it answers on its endpoint.
func start(t *testing.T, dataDir, name string, adapter *adapters) *instance {
	t.Helper()

	endpoint, err := daemon.SocketPath(dataDir)
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	options := daemon.Options{
		DataDir:   dataDir,
		Version:   "test",
		Name:      name,
		Platform:  model.PlatformMacOS,
		Keystore:  adapter.keystore,
		Clipboard: adapter.clipboard,
		Discovery: adapter.bus.Discovery(),
		Clock:     fake.NewClock(time.Now()),
		LogLevel:  slog.LevelDebug,
		Stderr:    testStderr(),
	}
	go func() { done <- daemon.Run(ctx, options) }()

	inst := &instance{endpoint: endpoint, cancel: cancel, done: done}
	t.Cleanup(func() {
		inst.stop(t)
	})
	waitRunning(t, inst)
	return inst
}

// stop stops the daemon and waits for it to return.
func (i *instance) stop(t *testing.T) {
	t.Helper()

	if i.stopped {
		if i.err != nil {
			t.Errorf("daemon returned: %v", i.err)
		}
		return
	}
	i.stopped = true

	i.cancel()
	select {
	case i.err = <-i.done:
		if i.err != nil {
			t.Errorf("daemon returned: %v", i.err)
		}
	case <-time.After(waitTimeout):
		t.Error("daemon did not stop")
	}
}

// waitRunning waits until the daemon answers on its endpoint.
func waitRunning(t *testing.T, inst *instance) {
	t.Helper()

	deadline := time.Now().Add(waitTimeout)
	for {
		status, err := status(t, inst)
		if err == nil && status.GetRunning() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon does not answer on %s: %v", inst.endpoint, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// status returns the status of the daemon.
func status(t *testing.T, inst *instance) (*localv1.GetStatusResponse, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()

	client := localconn.NewClient(inst.endpoint)
	response, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// TestRunServesStatus checks that a daemon starts, serves the local API on the
// endpoint of its data directory and stops cleanly.
func TestRunServesStatus(t *testing.T) {
	dataDir := shortDir(t)
	inst := start(t, dataDir, "test-device", newAdapters())

	status, err := status(t, inst)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status.GetVersion() != "test" {
		t.Errorf("version is %q, want %q", status.GetVersion(), "test")
	}
	if status.GetDeviceName() != "test-device" {
		t.Errorf("device name is %q, want %q", status.GetDeviceName(), "test-device")
	}
	if status.GetDeviceId() == "" {
		t.Error("device ID is empty")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(inst.endpoint)
		if err != nil {
			t.Fatalf("stat endpoint: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("endpoint mode is %v, want 0600", mode)
		}
	}

	inst.stop(t)
	if _, err := os.Stat(daemon.LockFile(dataDir)); err != nil {
		t.Errorf("lock file is missing: %v", err)
	}
}

// TestRunRefusesSecondInstance checks that a second daemon on the same data
// directory reports ErrRunning.
func TestRunRefusesSecondInstance(t *testing.T) {
	dataDir := shortDir(t)
	start(t, dataDir, "first", newAdapters())

	err := daemon.Run(context.Background(), daemon.Options{
		DataDir:   dataDir,
		Platform:  model.PlatformMacOS,
		Keystore:  fake.NewKeystore(),
		Clipboard: fake.NewClipboard(),
		Discovery: fake.NewBus().Discovery(),
	})
	if !errors.Is(err, daemon.ErrRunning) {
		t.Fatalf("second instance returned %v, want ErrRunning", err)
	}
}

// TestRunUsesStoredDeviceName checks that a device name stored through the
// local API is used by the next start.
func TestRunUsesStoredDeviceName(t *testing.T) {
	dataDir := shortDir(t)
	adapter := newAdapters()
	first := start(t, dataDir, "before", adapter)

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	client := localconn.NewClient(first.endpoint)
	name := "after"
	_, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{DeviceName: &name}))
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	first.stop(t)

	second := start(t, dataDir, "", adapter)
	status, err := status(t, second)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status.GetDeviceName() != name {
		t.Errorf("device name is %q, want %q", status.GetDeviceName(), name)
	}
}

// TestRunRefusesLiveEndpoint checks that a daemon exits when another process
// serves the endpoint without holding the lock file.
func TestRunRefusesLiveEndpoint(t *testing.T) {
	dataDir := shortDir(t)
	adapter := newAdapters()
	inst := start(t, dataDir, "first", adapter)

	// The second data directory is the same endpoint but a different lock
	// file, which is what a manually removed lock leaves behind.
	endpoint := inst.endpoint
	err := daemon.Run(context.Background(), daemon.Options{
		DataDir:   shortDir(t),
		Endpoint:  endpoint,
		Platform:  model.PlatformMacOS,
		Keystore:  fake.NewKeystore(),
		Clipboard: fake.NewClipboard(),
		Discovery: adapter.bus.Discovery(),
	})
	if !errors.Is(err, daemon.ErrRunning) {
		t.Fatalf("second instance returned %v, want ErrRunning", err)
	}
}

// TestNoClipboard checks that the clipboard without a pasteboard reports no
// content.
func TestNoClipboard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clipboard := daemon.NoClipboard()

	snapshots, err := clipboard.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if err := clipboard.Write(ctx, ports.Content{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	cancel()
	select {
	case _, ok := <-snapshots:
		if ok {
			t.Error("got a snapshot from a clipboard without a pasteboard")
		}
	case <-time.After(waitTimeout):
		t.Error("the watcher was not closed")
	}
}

// TestLogFile checks that the log file of a daemon is written below its data
// directory.
func TestLogFile(t *testing.T) {
	dataDir := shortDir(t)
	start(t, dataDir, "logger", newAdapters())

	content, err := os.ReadFile(daemon.LogFile(dataDir))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(content) == 0 {
		t.Error("log file is empty")
	}
	if dir := filepath.Dir(daemon.LogFile(dataDir)); dir != filepath.Join(dataDir, "logs") {
		t.Errorf("log file is in %q", dir)
	}
}

// TestSetClip checks that text from the local API reaches the clipboard
// pipeline.
func TestSetClip(t *testing.T) {
	dataDir := shortDir(t)
	inst := start(t, dataDir, "clip", newAdapters())

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	client := localconn.NewClient(inst.endpoint)

	set, err := client.SetClip(ctx, connect.NewRequest(&localv1.SetClipRequest{Text: "hello"}))
	if err != nil {
		t.Fatalf("set clip: %v", err)
	}
	if got := set.Msg.GetItem().GetId(); got == "" {
		t.Error("set clip returned no item")
	}

	got, err := client.GetClip(ctx, connect.NewRequest(&localv1.GetClipRequest{}))
	if err != nil {
		t.Fatalf("get clip: %v", err)
	}
	if got.Msg.GetItem().GetId() != set.Msg.GetItem().GetId() {
		t.Errorf("latest item is %q, want %q", got.Msg.GetItem().GetId(), set.Msg.GetItem().GetId())
	}
}

// TestRunReplacesStaleEndpoint checks that a socket left behind by a daemon
// that crashed does not keep the next daemon from serving.
func TestRunReplacesStaleEndpoint(t *testing.T) {
	if runtime.GOOS == "windows" {
		// A named pipe disappears with its last handle: no stale endpoint is left.
		t.Skip("Windows keeps no stale endpoint to replace")
	}

	dataDir := shortDir(t)
	endpoint, err := daemon.SocketPath(dataDir)
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// A crash skips the unlink, so the socket file stays without a listener.
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	if _, err := os.Lstat(endpoint); err != nil {
		t.Fatalf("the stale socket is missing: %v", err)
	}

	inst := start(t, dataDir, "after-crash", newAdapters())
	got, err := status(t, inst)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if got.GetDeviceName() != "after-crash" {
		t.Errorf("device name is %q, want %q", got.GetDeviceName(), "after-crash")
	}
}

// TestRunRemovesEndpointOnStop checks that a daemon that stops cleanly takes
// its endpoint away, so clients see it stopped instead of a dead socket.
func TestRunRemovesEndpointOnStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		// A named pipe disappears with its last handle.
		t.Skip("Windows keeps no endpoint to remove")
	}

	dataDir := shortDir(t)
	inst := start(t, dataDir, "stopping", newAdapters())
	inst.stop(t)

	if _, err := os.Lstat(inst.endpoint); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the endpoint is still there after the stop: %v", err)
	}
}

// TestApplicationLog checks that a daemon follows the marker of the journal and
// turns it off through the settings.
func TestApplicationLog(t *testing.T) {
	dataDir := shortDir(t)
	if err := os.MkdirAll(filepath.Dir(journal.Marker(dataDir)), 0o700); err != nil {
		t.Fatalf("create the log directory: %v", err)
	}
	if err := os.WriteFile(journal.Marker(dataDir), []byte("on\n"), 0o600); err != nil {
		t.Fatalf("write the marker: %v", err)
	}

	inst := start(t, dataDir, "journal", newAdapters())

	content, err := os.ReadFile(journal.Path(dataDir))
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if !strings.Contains(string(content), "starting daemon") {
		t.Errorf("the journal of a started daemon is empty: %q", content)
	}

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	client := localconn.NewClient(inst.endpoint)
	off := false
	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		JournalEnabled: &off,
	})); err != nil {
		t.Fatalf("turn the journal off: %v", err)
	}
	if _, err := os.Stat(journal.Marker(dataDir)); !os.IsNotExist(err) {
		t.Errorf("the marker of a disabled journal is kept: %v", err)
	}

	content, err = os.ReadFile(journal.Path(dataDir))
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if !strings.Contains(string(content), "application log disabled") {
		t.Errorf("the journal does not report that it was turned off: %q", content)
	}
}
