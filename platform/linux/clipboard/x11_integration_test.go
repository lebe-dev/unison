//go:build linux && x11clipboardtest

package clipboard

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/lebe-dev/unison/core/ports"
)

// testAtoms are the atoms a raw test client uses.
type testAtoms struct {
	clipboard  xproto.Atom
	targets    xproto.Atom
	utf8String xproto.Atom
	html       xproto.Atom
	atomType   xproto.Atom
	hint       xproto.Atom
	hintMIME   xproto.Atom
	stringAtom xproto.Atom
	timestamp  xproto.Atom
}

// rawClient is a minimal X11 clipboard client. It owns the CLIPBOARD selection
// and serves a fixed value, so the watcher sees a change from another client.
type rawClient struct {
	t       *testing.T
	conn    *xgb.Conn
	win     xproto.Window
	atoms   testAtoms
	mu      sync.Mutex
	plain   []byte
	html    []byte
	hint    bool
	request []string
}

// newRawClient connects to the display and starts serving selection requests.
func newRawClient(t *testing.T) *rawClient {
	t.Helper()
	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)
	screen := xproto.Setup(conn).DefaultScreen(conn)
	win, err := xproto.NewWindowId(conn)
	if err != nil {
		t.Fatalf("window id: %v", err)
	}
	err = xproto.CreateWindowChecked(conn, 0, win, screen.Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check()
	if err != nil {
		t.Fatalf("create window: %v", err)
	}
	client := &rawClient{t: t, conn: conn, win: win}
	client.atoms = testAtoms{
		clipboard:  internAtom(t, conn, atomCLIPBOARD),
		targets:    internAtom(t, conn, atomTARGETS),
		utf8String: internAtom(t, conn, atomUTF8String),
		html:       internAtom(t, conn, atomTextHTML),
		atomType:   internAtom(t, conn, atomATOMType),
		hint:       internAtom(t, conn, PasswordManagerHint),
		hintMIME:   internAtom(t, conn, PasswordManagerHintMIME),
		stringAtom: internAtom(t, conn, atomSTRING),
		timestamp:  internAtom(t, conn, atomTIMESTAMP),
	}
	go client.serve()
	return client
}

// internAtom returns the atom of a name.
func internAtom(t *testing.T, conn *xgb.Conn, name string) xproto.Atom {
	t.Helper()
	reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		t.Fatalf("intern %q: %v", name, err)
	}
	return reply.Atom
}

// own publishes the value and takes the selection.
func (r *rawClient) own(plain, html []byte, hint bool) {
	r.mu.Lock()
	r.plain, r.html, r.hint = plain, html, hint
	r.request = nil
	r.mu.Unlock()
	err := xproto.SetSelectionOwnerChecked(r.conn, r.win, r.atoms.clipboard, 0).Check()
	if err != nil {
		r.t.Fatalf("set selection owner: %v", err)
	}
}

// requested returns the target names the watcher asked for.
func (r *rawClient) requested() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.request...)
}

// serve answers selection requests with the current value.
func (r *rawClient) serve() {
	for {
		event, err := r.conn.WaitForEvent()
		if err != nil || event == nil {
			return
		}
		request, ok := event.(xproto.SelectionRequestEvent)
		if !ok {
			continue
		}
		r.answer(request)
	}
}

// answer replies to one selection request.
func (r *rawClient) answer(event xproto.SelectionRequestEvent) {
	property := event.Property
	if property == 0 {
		property = event.Target
	}
	r.mu.Lock()
	r.request = append(r.request, r.name(event.Target))
	plain, html, hint := r.plain, r.html, r.hint
	r.mu.Unlock()

	var (
		valueType xproto.Atom
		format    byte
		data      []byte
	)
	switch event.Target {
	case r.atoms.targets:
		valueType, format = r.atoms.atomType, 32
		targets := []atom{atom(r.atoms.timestamp), atom(r.atoms.targets), atom(r.atoms.utf8String)}
		if html != nil {
			targets = append(targets, atom(r.atoms.html))
		}
		if hint {
			targets = append(targets, atom(r.atoms.hint), atom(r.atoms.hintMIME))
		}
		data = encodeAtomList(targets)
	case r.atoms.utf8String:
		valueType, format, data = r.atoms.utf8String, 8, plain
	case r.atoms.html:
		valueType, format, data = r.atoms.html, 8, html
	case r.atoms.hint, r.atoms.hintMIME:
		valueType, format, data = r.atoms.hint, 8, []byte("secret")
	default:
		property = 0
	}
	if property != 0 {
		xproto.ChangeProperty(r.conn, xproto.PropModeReplace, event.Requestor, property, valueType, format, dataLen(data, format), data)
	}
	reply := xproto.SelectionNotifyEvent{
		Time:      event.Time,
		Requestor: event.Requestor,
		Selection: event.Selection,
		Target:    event.Target,
		Property:  property,
	}
	xproto.SendEvent(r.conn, false, event.Requestor, 0, string(reply.Bytes()))
}

// name returns the protocol name of a target for assertions.
func (r *rawClient) name(value xproto.Atom) string {
	reply, err := xproto.GetAtomName(r.conn, value).Reply()
	if err != nil {
		return ""
	}
	return reply.Name
}

// requireDisplay skips a test without an X display.
func requireDisplay(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("DISPLAY")) == "" {
		t.Skip("DISPLAY is not set")
	}
}

// watch starts a watcher and returns its snapshots.
func watch(t *testing.T) (*X11, <-chan ports.Snapshot) {
	t.Helper()
	clipboard, err := NewX11(X11Options{})
	if err != nil {
		t.Fatalf("NewX11: %v", err)
	}
	t.Cleanup(func() { _ = clipboard.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	snapshots, err := clipboard.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	return clipboard, snapshots
}

// waitConnected waits until the clipboard holds an X connection.
func waitConnected(t *testing.T, clipboard *X11) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		clipboard.state.connMu.Lock()
		conn := clipboard.state.conn
		clipboard.state.connMu.Unlock()
		if conn != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the clipboard did not connect to the X server")
}

// waitSnapshot returns the next snapshot.
func waitSnapshot(t *testing.T, snapshots <-chan ports.Snapshot) ports.Snapshot {
	t.Helper()
	select {
	case snapshot, ok := <-snapshots:
		if !ok {
			t.Fatal("the watcher closed")
		}
		return snapshot
	case <-time.After(5 * time.Second):
		t.Fatal("no clipboard snapshot")
	}
	return ports.Snapshot{}
}

// expectNoSnapshot fails when a snapshot arrives within the wait.
func expectNoSnapshot(t *testing.T, snapshots <-chan ports.Snapshot, wait time.Duration) {
	t.Helper()
	select {
	case snapshot := <-snapshots:
		t.Fatalf("unexpected snapshot %+v", snapshot)
	case <-time.After(wait):
	}
}

// representation returns the payload of a MIME type of a snapshot.
func representation(snapshot ports.Snapshot, mime string) ([]byte, bool) {
	for _, item := range snapshot.Content.Representations {
		if item.MIME == mime {
			return item.Data, true
		}
	}
	return nil, false
}

func TestWriteThenWatch(t *testing.T) {
	requireDisplay(t)
	writer, err := NewX11(X11Options{})
	if err != nil {
		t.Fatalf("NewX11: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if err := writer.Write(context.Background(), ports.Content{
		ItemID: "item-1",
		Representations: []ports.Representation{
			{MIME: MIMETextPlain, Data: []byte("first")},
			{MIME: MIMETextHTML, Data: []byte("<b>first</b>")},
		},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	watcher, snapshots := watch(t)
	waitConnected(t, watcher)

	// The content present at the start of watching is the baseline.
	expectNoSnapshot(t, snapshots, 300*time.Millisecond)

	// A second connection reads what the first one serves.
	reader, err := NewX11(X11Options{})
	if err != nil {
		t.Fatalf("NewX11: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if err := reader.Write(context.Background(), ports.Content{
		ItemID: "item-2",
		Representations: []ports.Representation{
			{MIME: MIMETextPlain, Data: []byte("second")},
			{MIME: MIMETextHTML, Data: []byte("<i>second</i>")},
		},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	snapshot := waitSnapshot(t, snapshots)
	if !snapshot.OwnedByUs {
		t.Fatal("OwnedByUs = false, want true for content with the marker")
	}
	plain, ok := representation(snapshot, MIMETextPlain)
	if !ok || string(plain) != "second" {
		t.Fatalf("plain = %q (%v), want %q", plain, ok, "second")
	}
	html, ok := representation(snapshot, MIMETextHTML)
	if !ok || string(html) != "<i>second</i>" {
		t.Fatalf("html = %q (%v), want %q", html, ok, "<i>second</i>")
	}
}

func TestWriteLargeValueThroughINCR(t *testing.T) {
	requireDisplay(t)
	writer, err := NewX11(X11Options{})
	if err != nil {
		t.Fatalf("NewX11: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	watcher, snapshots := watch(t)
	waitConnected(t, watcher)

	large := bytes.Repeat([]byte("0123456789abcdef"), incrThreshold/16+1024)
	if err := writer.Write(context.Background(), ports.Content{
		Representations: []ports.Representation{{MIME: MIMETextPlain, Data: large}},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	snapshot := waitSnapshot(t, snapshots)
	plain, ok := representation(snapshot, MIMETextPlain)
	if !ok {
		t.Fatal("no plain representation")
	}
	if !bytes.Equal(plain, large) {
		t.Fatalf("plain value of %d bytes does not match the written %d bytes", len(plain), len(large))
	}
}

func TestWatchForeignOwner(t *testing.T) {
	requireDisplay(t)
	watcher, snapshots := watch(t)
	waitConnected(t, watcher)
	client := newRawClient(t)
	client.own([]byte("from another client"), nil, false)
	snapshot := waitSnapshot(t, snapshots)
	if snapshot.OwnedByUs {
		t.Fatal("OwnedByUs = true, want false for a foreign write")
	}
	plain, ok := representation(snapshot, MIMETextPlain)
	if !ok || string(plain) != "from another client" {
		t.Fatalf("plain = %q (%v), want %q", plain, ok, "from another client")
	}
}

func TestWatchSensitive(t *testing.T) {
	requireDisplay(t)
	watcher, snapshots := watch(t)
	waitConnected(t, watcher)
	client := newRawClient(t)
	client.own([]byte("password"), nil, true)
	snapshot := waitSnapshot(t, snapshots)
	if !snapshot.Sensitive {
		t.Fatal("Sensitive = false, want true for the password manager hint")
	}
	if len(snapshot.Content.Representations) != 0 {
		t.Fatalf("sensitive snapshot carries %d representations, want none", len(snapshot.Content.Representations))
	}
	for _, target := range client.requested() {
		if target == atomUTF8String {
			t.Fatal("the watcher read the payload of sensitive content")
		}
	}
}

func TestWatchHTMLUTF16(t *testing.T) {
	requireDisplay(t)
	watcher, snapshots := watch(t)
	waitConnected(t, watcher)
	client := newRawClient(t)
	// A UTF-16 little endian HTML value with a byte order mark.
	html := []byte{0xFF, 0xFE}
	for _, unit := range []byte("<b>hi</b>") {
		html = append(html, unit, 0)
	}
	client.own(nil, html, false)
	snapshot := waitSnapshot(t, snapshots)
	value, ok := representation(snapshot, MIMETextHTML)
	if !ok || string(value) != "<b>hi</b>" {
		t.Fatalf("html = %q (%v), want %q", value, ok, "<b>hi</b>")
	}
}
