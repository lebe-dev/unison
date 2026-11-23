//go:build linux

package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/linux/clipboard"
)

// ErrClipboardUnavailable reports a clipboard that has no display behind it.
var ErrClipboardUnavailable = errors.New("daemon: the system clipboard is not available")

// managedClipboard reaches the clipboard when the display appears, so the
// daemon survives a session that starts after it.
type managedClipboard struct {
	mode string
	env  clipboardEnvironment

	mu      sync.Mutex
	inner   ports.Clipboard
	backend string
	note    string
	subs    map[chan ports.Snapshot]struct{}
	changed chan struct{}

	forward sync.Once
}

var _ ports.Clipboard = (*managedClipboard)(nil)

// newManagedClipboard returns a clipboard of the mode with the display of the
// environment, when it answers now.
func newManagedClipboard(mode string, env clipboardEnvironment) *managedClipboard {
	c := &managedClipboard{
		mode:    mode,
		env:     env,
		backend: ClipboardNone,
		subs:    make(map[chan ports.Snapshot]struct{}),
		changed: make(chan struct{}, 1),
	}
	c.inner, c.backend, c.note = c.resolve()
	return c
}

// resolve returns the clipboard of the session, or nil and the reason it is not
// available.
func (c *managedClipboard) resolve() (ports.Clipboard, string, string) {
	if c.mode != ClipboardX11 && c.env.getenv("WAYLAND_DISPLAY") != "" {
		wayland, err := c.env.newWayland()
		if err == nil {
			return wayland, ClipboardWayland, ""
		}
		if c.mode == ClipboardWayland {
			return nil, ClipboardNone, waylandNote(err)
		}
	}
	if c.mode == ClipboardWayland {
		return nil, ClipboardNone, "WAYLAND_DISPLAY is not set"
	}
	display := strings.TrimSpace(c.env.getenv("DISPLAY"))
	if display == "" {
		return nil, ClipboardNone, "DISPLAY is not set"
	}
	if err := c.env.probe(context.Background(), display); err != nil {
		return nil, ClipboardNone, "the X server is not reachable"
	}
	x11, err := c.env.newX11(display)
	if err != nil {
		return nil, ClipboardNone, "the X11 clipboard is not available"
	}
	return x11, ClipboardX11, ""
}

// waylandNote explains why the Wayland clipboard is not available.
func waylandNote(err error) string {
	if errors.Is(err, clipboard.ErrNoDataControl) {
		return "the compositor does not offer the data-control protocol"
	}
	return "the Wayland clipboard is not available"
}

// ClipboardStatus reports the backend of the clipboard and the reason of the
// manual mode.
func (c *managedClipboard) ClipboardStatus() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.backend, c.note
}

// StartupWarning returns the manual mode of a daemon without a display.
func (c *managedClipboard) StartupWarning(context.Context) string {
	backend, note := c.ClipboardStatus()
	if backend != ClipboardNone {
		return ""
	}
	if note == "" {
		note = "the display is not available"
	}
	return "the clipboard is not available, the daemon runs in manual mode: " + note
}

// RetryClipboard tries the display again and reports whether the clipboard is
// available now.
func (c *managedClipboard) RetryClipboard() bool {
	c.mu.Lock()
	available := c.inner != nil
	c.mu.Unlock()
	if available {
		return true
	}

	inner, backend, note := c.resolve()
	c.mu.Lock()
	if inner == nil {
		c.backend, c.note = backend, note
		c.mu.Unlock()
		return false
	}
	c.inner, c.backend, c.note = inner, backend, ""
	c.mu.Unlock()

	c.notifyChange()
	return true
}

// Watch delivers a snapshot for every change of the clipboard, also after the
// display appeared.
func (c *managedClipboard) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make(chan ports.Snapshot, clipboardWatchBuffer)
	c.mu.Lock()
	c.subs[out] = struct{}{}
	c.mu.Unlock()

	c.forward.Do(func() { go c.forwardSnapshots(ctx) })
	go func() {
		<-ctx.Done()
		c.mu.Lock()
		if _, ok := c.subs[out]; ok {
			delete(c.subs, out)
			close(out)
		}
		c.mu.Unlock()
	}()
	return out, nil
}

// Write copies content to the clipboard of the display.
func (c *managedClipboard) Write(ctx context.Context, content ports.Content) error {
	c.mu.Lock()
	inner := c.inner
	c.mu.Unlock()
	if inner == nil {
		return ErrClipboardUnavailable
	}
	return inner.Write(ctx, content)
}

// forwardSnapshots forwards the snapshots of the current clipboard until ctx
// ends. Replacing the clipboard restarts the watch.
func (c *managedClipboard) forwardSnapshots(ctx context.Context) {
	for {
		c.mu.Lock()
		inner := c.inner
		c.mu.Unlock()

		if inner == nil {
			select {
			case <-ctx.Done():
				return
			case <-c.changed:
				continue
			}
		}

		watchCtx, stop := context.WithCancel(ctx)
		snapshots, err := inner.Watch(watchCtx)
		if err != nil {
			stop()
			select {
			case <-ctx.Done():
				return
			case <-c.changed:
			}
			continue
		}
		alive := c.pump(ctx, snapshots)
		stop()
		if !alive {
			return
		}
	}
}

// pump forwards snapshots until the clipboard is replaced or the channel ends.
// It reports false when ctx ended.
func (c *managedClipboard) pump(ctx context.Context, snapshots <-chan ports.Snapshot) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-c.changed:
			return true
		case snapshot, ok := <-snapshots:
			if !ok {
				return true
			}
			c.publish(snapshot)
		}
	}
}

// publish delivers a snapshot to every watcher. A watcher that cannot keep up
// loses the snapshot.
func (c *managedClipboard) publish(snapshot ports.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

// notifyChange tells the forwarder that the clipboard changed.
func (c *managedClipboard) notifyChange() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}
