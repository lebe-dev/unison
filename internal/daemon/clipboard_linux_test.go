//go:build linux

package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/linux/clipboard"
)

// testClipboard is a clipboard of a test that records writes and lets the test
// send snapshots.
type testClipboard struct {
	mu      sync.Mutex
	watch   chan struct{}
	subs    []chan ports.Snapshot
	written []ports.Content
	err     error
}

// Watch registers the watcher of the test.
func (c *testClipboard) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	if c.err != nil {
		return nil, c.err
	}
	out := make(chan ports.Snapshot, 4)
	c.mu.Lock()
	c.subs = append(c.subs, out)
	if c.watch != nil {
		select {
		case c.watch <- struct{}{}:
		default:
		}
	}
	c.mu.Unlock()
	go func() {
		<-ctx.Done()
		c.mu.Lock()
		for i, sub := range c.subs {
			if sub == out {
				c.subs = append(c.subs[:i], c.subs[i+1:]...)
				close(out)
				break
			}
		}
		c.mu.Unlock()
	}()
	return out, nil
}

// Write records the content of the test.
func (c *testClipboard) Write(_ context.Context, content ports.Content) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written = append(c.written, content)
	return nil
}

// emit sends a snapshot to every watcher.
func (c *testClipboard) emit(snapshot ports.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sub := range c.subs {
		sub <- snapshot
	}
}

// waitWatchers waits until the clipboard has the given number of watchers.
func (c *testClipboard) waitWatchers(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := len(c.subs)
		c.mu.Unlock()
		if got >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("clipboard has %d watchers, want %d", len(c.subs), count)
}

// env returns an environment with the given variables.
func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// TestClipboardWithoutDisplay checks the manual mode of a session without a
// display.
func TestClipboardWithoutDisplay(t *testing.T) {
	c := newManagedClipboard(ClipboardAuto, clipboardEnvironment{
		getenv: env(nil),
		probe: func(context.Context, string) error {
			t.Fatal("the probe ran without a display")
			return nil
		},
	})

	backend, note := c.ClipboardStatus()
	if backend != ClipboardNone || note != "DISPLAY is not set" {
		t.Fatalf("status is %q %q", backend, note)
	}
	if warning := c.StartupWarning(context.Background()); warning == "" {
		t.Fatal("the manual mode has no warning")
	}
	if err := c.Write(context.Background(), ports.Content{}); !errors.Is(err, ErrClipboardUnavailable) {
		t.Fatalf("write = %v, want %v", err, ErrClipboardUnavailable)
	}
}

// TestClipboardX11 checks the clipboard of an X server that answers.
func TestClipboardX11(t *testing.T) {
	inner := &testClipboard{}
	c := newManagedClipboard(ClipboardAuto, clipboardEnvironment{
		getenv: env(map[string]string{"DISPLAY": ":0"}),
		probe:  func(context.Context, string) error { return nil },
		newX11: func(display string) (ports.Clipboard, error) {
			if display != ":0" {
				t.Fatalf("display is %q, want :0", display)
			}
			return inner, nil
		},
	})

	backend, note := c.ClipboardStatus()
	if backend != ClipboardX11 || note != "" {
		t.Fatalf("status is %q %q", backend, note)
	}
	if warning := c.StartupWarning(context.Background()); warning != "" {
		t.Fatalf("warning of a working clipboard is %q", warning)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshots, err := c.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	inner.waitWatchers(t, 1)

	const text = "hello"
	inner.emit(ports.Snapshot{Content: ports.Content{Representations: []ports.Representation{{MIME: "text/plain", Data: []byte(text)}}}})
	select {
	case snapshot := <-snapshots:
		if got := string(snapshot.Content.Representations[0].Data); got != text {
			t.Fatalf("snapshot is %q, want %q", got, text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the snapshot did not arrive")
	}

	if err := c.Write(ctx, ports.Content{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.written) != 1 {
		t.Fatalf("%d writes, want 1", len(inner.written))
	}
}

// TestClipboardPrefersWayland checks the Wayland backend of an auto session.
func TestClipboardPrefersWayland(t *testing.T) {
	wayland := &testClipboard{}
	c := newManagedClipboard(ClipboardAuto, clipboardEnvironment{
		getenv: env(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}),
		probe: func(context.Context, string) error {
			t.Fatal("the probe ran for a Wayland session")
			return nil
		},
		newX11: func(string) (ports.Clipboard, error) {
			t.Fatal("the X11 clipboard was used for a Wayland session")
			return nil, nil
		},
		newWayland: func() (ports.Clipboard, error) { return wayland, nil },
	})

	backend, note := c.ClipboardStatus()
	if backend != ClipboardWayland || note != "" {
		t.Fatalf("status is %q %q", backend, note)
	}
}

// TestClipboardFallsBackFromWayland checks that a session without data-control
// uses XWayland.
func TestClipboardFallsBackFromWayland(t *testing.T) {
	c := newManagedClipboard(ClipboardAuto, clipboardEnvironment{
		getenv: env(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}),
		probe:  func(context.Context, string) error { return nil },
		newX11: func(string) (ports.Clipboard, error) { return &testClipboard{}, nil },
		newWayland: func() (ports.Clipboard, error) {
			return nil, clipboard.ErrNoDataControl
		},
	})

	if backend, _ := c.ClipboardStatus(); backend != ClipboardX11 {
		t.Fatalf("backend is %q, want %q", backend, ClipboardX11)
	}
}

// TestClipboardX11ModeIgnoresWayland checks the explicit X11 mode in a Wayland
// session.
func TestClipboardX11ModeIgnoresWayland(t *testing.T) {
	c := newManagedClipboard(ClipboardX11, clipboardEnvironment{
		getenv: env(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":1"}),
		probe:  func(context.Context, string) error { return nil },
		newX11: func(string) (ports.Clipboard, error) { return &testClipboard{}, nil },
		newWayland: func() (ports.Clipboard, error) {
			t.Fatal("the Wayland clipboard was used in the X11 mode")
			return nil, nil
		},
	})

	if backend, _ := c.ClipboardStatus(); backend != ClipboardX11 {
		t.Fatalf("backend is %q, want %q", backend, ClipboardX11)
	}
}

// TestClipboardAppearsLater checks the switch from the manual mode to the
// working clipboard without a restart.
func TestClipboardAppearsLater(t *testing.T) {
	inner := &testClipboard{}
	display := false
	c := newManagedClipboard(ClipboardAuto, clipboardEnvironment{
		getenv: env(map[string]string{"DISPLAY": ":0"}),
		probe: func(context.Context, string) error {
			if !display {
				return errors.New("no server")
			}
			return nil
		},
		newX11: func(string) (ports.Clipboard, error) { return inner, nil },
	})

	backend, note := c.ClipboardStatus()
	if backend != ClipboardNone || note != "the X server is not reachable" {
		t.Fatalf("status is %q %q", backend, note)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshots, err := c.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	if c.RetryClipboard() {
		t.Fatal("the clipboard is available without a display")
	}
	display = true
	if !c.RetryClipboard() {
		t.Fatal("the clipboard did not appear with the display")
	}
	backend, note = c.ClipboardStatus()
	if backend != ClipboardX11 || note != "" {
		t.Fatalf("status after the display appeared is %q %q", backend, note)
	}

	inner.waitWatchers(t, 1)
	inner.emit(ports.Snapshot{Content: ports.Content{ItemID: "one"}})
	select {
	case snapshot := <-snapshots:
		if snapshot.Content.ItemID != "one" {
			t.Fatalf("snapshot item is %q", snapshot.Content.ItemID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the snapshot of the new clipboard did not arrive")
	}

	if err := c.Write(ctx, ports.Content{}); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestNewClipboardModes checks the modes of the clipboard flag.
func TestNewClipboardModes(t *testing.T) {
	if clip, err := NewClipboard(ClipboardNone); err != nil {
		t.Fatalf("none: %v", err)
	} else if _, ok := clip.(noClipboard); !ok {
		t.Fatalf("none returned %T", clip)
	}
	clip, err := NewClipboard(ClipboardWayland)
	if err != nil {
		t.Fatalf("wayland: %v", err)
	}
	managed, ok := clip.(*managedClipboard)
	if !ok {
		t.Fatalf("wayland returned %T", clip)
	}
	backend, note := managed.ClipboardStatus()
	if backend != ClipboardWayland && (backend != ClipboardNone || note == "") {
		t.Fatalf("wayland status is %q %q", backend, note)
	}
	if _, err := NewClipboard("bogus"); err == nil {
		t.Fatal("an unknown mode has no error")
	}
}

// TestClipboardWaylandModeReportsTheReason checks the status of the explicit
// Wayland mode without a usable compositor.
func TestClipboardWaylandModeReportsTheReason(t *testing.T) {
	c := newManagedClipboard(ClipboardWayland, clipboardEnvironment{
		getenv:     env(nil),
		newWayland: func() (ports.Clipboard, error) { return nil, clipboard.ErrNoDataControl },
	})
	backend, note := c.ClipboardStatus()
	if backend != ClipboardNone || note != "WAYLAND_DISPLAY is not set" {
		t.Fatalf("status without a display is %q %q", backend, note)
	}

	c = newManagedClipboard(ClipboardWayland, clipboardEnvironment{
		getenv:     env(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
		newWayland: func() (ports.Clipboard, error) { return nil, clipboard.ErrNoDataControl },
	})
	backend, note = c.ClipboardStatus()
	if backend != ClipboardNone || note != "the compositor does not offer the data-control protocol" {
		t.Fatalf("status without data-control is %q %q", backend, note)
	}
}
