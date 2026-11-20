//go:build waylandclipboardtest && linux

package clipboard

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// The tests need a running compositor that offers a data-control protocol.
// Inside a session:
//
//	go test -tags waylandclipboardtest ./platform/linux/clipboard/
//
// A headless wlroots compositor works as well:
//
//	WLR_BACKENDS=headless WLR_LIBINPUT_NO_DEVICES=1 sway -d &
//	XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-1 \
//	    wayland.test -test.v -test.run Wayland

const (
	// watchTimeout bounds one expected clipboard change.
	watchTimeout = 10 * time.Second
	// baselineWindow is how long a watcher must stay silent about the content
	// present at its start.
	baselineWindow = 700 * time.Millisecond
)

// testClipboard returns a clipboard of the compositor. The test fails when the
// session offers no data-control protocol, because that is the subject.
func testClipboard(t *testing.T) *Wayland {
	t.Helper()
	clip, err := NewWayland(WaylandOptions{})
	if err != nil {
		t.Fatalf("this test needs a compositor with a data-control protocol: %v", err)
	}
	t.Cleanup(func() { _ = clip.Close() })
	return clip
}

// writeText puts the text into the clipboard of the backend.
func writeText(t *testing.T, clip *Wayland, text string) {
	t.Helper()
	err := clip.Write(context.Background(), ports.Content{
		ItemID: "test-item",
		Representations: []ports.Representation{{
			MIME: MIMETextPlain,
			Data: []byte(text),
		}},
	})
	if err != nil {
		t.Fatalf("write %q: %v", text, err)
	}
}

// nextSnapshot returns the next snapshot of the watcher.
func nextSnapshot(t *testing.T, snapshots <-chan ports.Snapshot) ports.Snapshot {
	t.Helper()
	select {
	case snapshot, ok := <-snapshots:
		if !ok {
			t.Fatal("the watch stopped")
		}
		return snapshot
	case <-time.After(watchTimeout):
		t.Fatal("no clipboard change arrived")
	}
	return ports.Snapshot{}
}

// plainOf returns the plain text of a snapshot.
func plainOf(t *testing.T, snapshot ports.Snapshot) string {
	t.Helper()
	for _, representation := range snapshot.Content.Representations {
		if representation.MIME == MIMETextPlain {
			return string(representation.Data)
		}
	}
	t.Fatalf("the snapshot has no plain text: %v", snapshot.Content.Representations)
	return ""
}

// TestWaylandReadSeesForeignWrite checks the read of the selection another
// backend set.
func TestWaylandReadSeesText(t *testing.T) {
	writer := testClipboard(t)
	reader := testClipboard(t)
	const text = "hello from wayland"
	writeText(t, writer, text)

	report, err := reader.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := plainOf(t, report.Snapshot); got != text {
		t.Fatalf("text is %q, want %q", got, text)
	}
	if !report.OwnerIsUs || !report.Snapshot.OwnedByUs {
		t.Fatal("an own write is not marked as own")
	}
	if !containsMIME(report.Targets, MarkerTarget) {
		t.Fatalf("the targets are %v", report.Targets)
	}
}

// TestWaylandWatchReportsChange checks that a watcher reports the writes of
// another backend.
func TestWaylandWatchReportsChange(t *testing.T) {
	watcher := testClipboard(t)
	writer := testClipboard(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshots, err := watcher.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	writeText(t, writer, "one")
	if got := plainOf(t, nextSnapshot(t, snapshots)); got != "one" {
		t.Fatalf("first change is %q", got)
	}
	writeText(t, writer, "two")
	if got := plainOf(t, nextSnapshot(t, snapshots)); got != "two" {
		t.Fatalf("second change is %q", got)
	}
}

// TestWaylandWatchSkipsStartingContent checks the baseline of the watcher.
func TestWaylandWatchSkipsStartingContent(t *testing.T) {
	writer := testClipboard(t)
	writeText(t, writer, "before the watch")
	watcher := testClipboard(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshots, err := watcher.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	select {
	case snapshot := <-snapshots:
		t.Fatalf("the starting content was reported: %q", plainOf(t, snapshot))
	case <-time.After(baselineWindow):
	}

	writeText(t, writer, "after the watch")
	if got := plainOf(t, nextSnapshot(t, snapshots)); got != "after the watch" {
		t.Fatalf("the change is %q", got)
	}
}

// TestWaylandRichContent checks that HTML and an image travel unchanged.
func TestWaylandRichContent(t *testing.T) {
	writer := testClipboard(t)
	reader := testClipboard(t)
	png := encodeTestImage(t, MIMEImagePNG)
	html := []byte("<b>rich</b>")
	err := writer.Write(context.Background(), ports.Content{
		Representations: []ports.Representation{
			{MIME: MIMETextPlain, Data: []byte("rich")},
			{MIME: MIMETextHTML, Data: html},
			{MIME: MIMEImagePNG, Data: png},
		},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	report, err := reader.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := plainOf(t, report.Snapshot); got != "rich" {
		t.Fatalf("text is %q", got)
	}
	if got := representationOf(t, report.Snapshot, MIMETextHTML); string(got) != string(html) {
		t.Fatalf("html is %q", got)
	}
	if got := representationOf(t, report.Snapshot, MIMEImagePNG); string(got) != string(png) {
		t.Fatalf("png of %d bytes does not match %d bytes", len(got), len(png))
	}
}

// TestWaylandFiles checks that a copied file list travels as paths.
func TestWaylandFiles(t *testing.T) {
	writer := testClipboard(t)
	reader := testClipboard(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "one.txt")
	second := filepath.Join(dir, "two.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	err := writer.Write(context.Background(), ports.Content{
		Representations: []ports.Representation{
			{MIME: MIMEUnisonFile, FilePath: first},
			{MIME: MIMEUnisonFile, FilePath: second},
		},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	report, err := reader.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var paths []string
	for _, representation := range report.Snapshot.Content.Representations {
		if representation.FilePath != "" {
			paths = append(paths, representation.FilePath)
		}
	}
	if len(paths) != 2 || paths[0] != first || paths[1] != second {
		t.Fatalf("files are %v", paths)
	}
}

// TestWaylandLosingOwnership checks that a write of another backend takes the
// selection over.
func TestWaylandLosingOwnership(t *testing.T) {
	first := testClipboard(t)
	second := testClipboard(t)
	writeText(t, first, "first")
	writeText(t, second, "second")

	report, err := first.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := plainOf(t, report.Snapshot); got != "second" {
		t.Fatalf("the selection is %q", got)
	}
}

// representationOf returns the data of one MIME type.
func representationOf(t *testing.T, snapshot ports.Snapshot, mime string) []byte {
	t.Helper()
	for _, representation := range snapshot.Content.Representations {
		if representation.MIME == mime {
			return representation.Data
		}
	}
	t.Fatalf("the snapshot has no %s", mime)
	return nil
}

// containsMIME reports whether the list has the MIME type.
func containsMIME(targets []string, mime string) bool {
	for _, target := range targets {
		if target == mime {
			return true
		}
	}
	return false
}

// TestWaylandSessionProtocol reports the data-control protocol the session
// offers, which tells a failure of the other tests apart from a missing
// protocol.
func TestWaylandSessionProtocol(t *testing.T) {
	clip := testClipboard(t)
	clip.state.connMu.Lock()
	conn := clip.state.conn
	clip.state.connMu.Unlock()
	if conn == nil {
		t.Fatal("the clipboard has no connection")
	}
	t.Logf("the compositor offers %s", conn.protocol)
}
