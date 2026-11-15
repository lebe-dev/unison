//go:build windows && clipboardtest

package clipboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	testOpenTimeout = 2 * time.Second
	testTimeout     = 3 * time.Second
	testChangeWait  = 500 * time.Millisecond
)

// TestMain puts back the plain text the tests replace. The adapter reads text
// only, so the remaining formats of the user clipboard are not preserved.
func TestMain(m *testing.M) {
	text, ok := readPlainText()
	code := m.Run()
	if ok {
		_ = writePlainText(text)
	}
	os.Exit(code)
}

func newClipboard(t *testing.T) *Clipboard {
	t.Helper()
	c, err := New(Options{OpenTimeout: testOpenTimeout})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func watch(t *testing.T) (<-chan ports.Snapshot, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	snapshots, err := newClipboard(t).Watch(ctx)
	if err != nil {
		cancel()
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(cancel)
	return snapshots, cancel
}

func next(t *testing.T, snapshots <-chan ports.Snapshot) ports.Snapshot {
	t.Helper()
	select {
	case snapshot, ok := <-snapshots:
		if !ok {
			t.Fatal("snapshot channel closed")
		}
		return snapshot
	case <-time.After(testTimeout):
		t.Fatal("no snapshot within the timeout")
	}
	return ports.Snapshot{}
}

func expectNone(t *testing.T, snapshots <-chan ports.Snapshot, wait time.Duration) {
	t.Helper()
	select {
	case snapshot, ok := <-snapshots:
		if ok {
			t.Fatalf("unexpected snapshot with %d representations", len(snapshot.Content.Representations))
		}
	case <-time.After(wait):
	}
}

func TestWriteTextAndHTML(t *testing.T) {
	snapshots, _ := watch(t)
	c := newClipboard(t)

	content := ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte("hello")},
		{MIME: MIMETextHTML, Data: []byte("<b>hello</b>")},
	}}
	if err := c.Write(context.Background(), content); err != nil {
		t.Fatalf("Write: %v", err)
	}

	snapshot := next(t, snapshots)
	if !snapshot.OwnedByUs {
		t.Fatal("OwnedByUs = false, want true")
	}
	if snapshot.Sensitive {
		t.Fatal("Sensitive = true, want false")
	}
	representations := snapshot.Content.Representations
	if len(representations) != 2 {
		t.Fatalf("got %d representations, want 2", len(representations))
	}
	if representations[0].MIME != MIMETextPlain || string(representations[0].Data) != "hello" {
		t.Fatalf("first representation is %s %q", representations[0].MIME, representations[0].Data)
	}
	if representations[1].MIME != MIMETextHTML || string(representations[1].Data) != "<b>hello</b>" {
		t.Fatalf("second representation is %s %q", representations[1].MIME, representations[1].Data)
	}
}

func TestWriteHTMLOnly(t *testing.T) {
	snapshots, _ := watch(t)
	c := newClipboard(t)

	content := ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextHTML, Data: []byte("<i>only</i>")},
	}}
	if err := c.Write(context.Background(), content); err != nil {
		t.Fatalf("Write: %v", err)
	}

	snapshot := next(t, snapshots)
	representations := snapshot.Content.Representations
	if len(representations) != 1 || representations[0].MIME != MIMETextHTML {
		t.Fatalf("got %d representations, want one HTML", len(representations))
	}
	if !bytes.Equal(representations[0].Data, []byte("<i>only</i>")) {
		t.Fatalf("html = %q, want the written fragment", representations[0].Data)
	}
}

func TestSensitiveMarkers(t *testing.T) {
	markers := map[string][]byte{
		formatExclude: nil,
		formatHistory: binary.LittleEndian.AppendUint32(nil, 0),
		formatCloud:   binary.LittleEndian.AppendUint32(nil, 0),
	}
	for marker, value := range markers {
		t.Run(marker, func(t *testing.T) {
			snapshots, _ := watch(t)
			writeWithFormat(t, "secret", marker, value)

			snapshot := next(t, snapshots)
			if !snapshot.Sensitive {
				t.Fatal("Sensitive = false, want true")
			}
			if len(snapshot.Content.Representations) != 0 {
				t.Fatalf("got %d representations for sensitive content, want none", len(snapshot.Content.Representations))
			}
		})
	}
}

func TestHistoryMarkerAllowsContent(t *testing.T) {
	snapshots, _ := watch(t)
	writeWithFormat(t, "shared", formatHistory, binary.LittleEndian.AppendUint32(nil, 1))

	snapshot := next(t, snapshots)
	if snapshot.Sensitive {
		t.Fatal("Sensitive = true for content that may enter the history")
	}
	if len(snapshot.Content.Representations) != 1 {
		t.Fatalf("got %d representations, want one", len(snapshot.Content.Representations))
	}
}

func TestCurrentContentIsNotReported(t *testing.T) {
	if err := writePlainText([]byte("before the watch")); err != nil {
		t.Fatalf("write: %v", err)
	}
	snapshots, _ := watch(t)
	expectNone(t, snapshots, testChangeWait)

	if err := writePlainText([]byte("after the watch")); err != nil {
		t.Fatalf("write: %v", err)
	}
	snapshot := next(t, snapshots)
	if got := string(snapshot.Content.Representations[0].Data); got != "after the watch" {
		t.Fatalf("snapshot content = %q, want %q", got, "after the watch")
	}
}

func TestWatchStopsWithContext(t *testing.T) {
	snapshots, cancel := watch(t)
	cancel()

	select {
	case _, ok := <-snapshots:
		if ok {
			t.Fatal("received a snapshot after cancel")
		}
	case <-time.After(testTimeout):
		t.Fatal("snapshot channel was not closed")
	}
}

func TestWriteClearsPreviousContent(t *testing.T) {
	snapshots, _ := watch(t)
	if err := writePlainText([]byte("previous")); err != nil {
		t.Fatalf("write: %v", err)
	}
	next(t, snapshots)

	c := newClipboard(t)
	content := ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte("current")},
	}}
	if err := c.Write(context.Background(), content); err != nil {
		t.Fatalf("Write: %v", err)
	}

	snapshot := next(t, snapshots)
	representations := snapshot.Content.Representations
	if len(representations) != 1 || string(representations[0].Data) != "current" {
		t.Fatalf("got %d representations, want only %q", len(representations), "current")
	}
}

// readPlainText returns the plain text on the clipboard.
func readPlainText() ([]byte, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(testOpenTimeout); err != nil {
		return nil, false
	}
	defer closeClipboard()
	payload, ok := clipboardData(cfUnicodeText)
	if !ok {
		return nil, false
	}
	return decodeText(payload), true
}

// writePlainText replaces the clipboard with plain text.
func writePlainText(text []byte) error {
	return grabClipboard(func() error {
		if err := emptyClipboard(); err != nil {
			return err
		}
		return putClipboardData(cfUnicodeText, encodeText(text))
	})
}

// writeWithFormat replaces the clipboard with text and an extra format.
func writeWithFormat(t *testing.T, text, name string, value []byte) {
	t.Helper()
	format := registerFormat(name)
	err := grabClipboard(func() error {
		if err := emptyClipboard(); err != nil {
			return err
		}
		if err := putClipboardData(cfUnicodeText, encodeText([]byte(text))); err != nil {
			return err
		}
		return putClipboardData(format, value)
	})
	if err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// grabClipboard runs change on an open clipboard of the calling thread.
func grabClipboard(change func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(testOpenTimeout); err != nil {
		return err
	}
	defer closeClipboard()
	return change()
}
