//go:build linux

package clipboard

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SpinningVinyl/go-wayland/wayland/client"

	"github.com/lebe-dev/unison/core/ports"
)

// offerID is an object identifier a compositor may announce. The display and
// the registry use the first small identifiers, so the tests stay above them.
const offerID = 0x1000

// readFragment returns a recorded body of a data-control event.
func readFragment(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func newTestContext(t *testing.T) *client.Context {
	t.Helper()
	path := startFakeCompositor(t)
	display, err := client.Connect(path)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = display.Context().Close() })
	return display.Context()
}

// startFakeCompositor listens on a socket and answers the global listing and
// the sync round trip. The advertised globals are the interfaces a client sees.
func startFakeCompositor(t *testing.T, globals ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayland-0")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go serveFakeCompositor(listener, globals)
	return path
}

func serveFakeCompositor(listener net.Listener, globals []string) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	for {
		header := make([]byte, 8)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		sender := binary.LittleEndian.Uint32(header[0:4])
		opcodeAndSize := binary.LittleEndian.Uint32(header[4:8])
		body := make([]byte, int(opcodeAndSize>>16)-8)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		// Only requests of the display matter here.
		if sender != 1 {
			continue
		}
		switch opcodeAndSize & 0xffff {
		case 0:
			writeFakeEvent(conn, binary.LittleEndian.Uint32(body), 0, u32Arg(0))
		case 1:
			registry := binary.LittleEndian.Uint32(body)
			for index, global := range globals {
				writeFakeEvent(conn, registry, 0, globalBody(uint32(index+1), global))
			}
		}
	}
}

// globalBody builds the body of a wl_registry.global event.
func globalBody(name uint32, iface string) []byte {
	padded := client.PaddedLen(len(iface) + 1)
	str := make([]byte, 4+padded)
	client.PutString(str, iface, padded)
	body := append(u32Arg(name), str...)
	return append(body, u32Arg(1)...)
}

// writeFakeEvent writes one event of the protocol.
func writeFakeEvent(conn net.Conn, id, opcode uint32, body []byte) {
	buf := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(buf[0:4], id)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(8+len(body))<<16|opcode&0xffff)
	copy(buf[8:], body)
	_, _ = conn.Write(buf)
}

// TestConnectWithoutDataControl checks the error of a compositor that offers no
// data-control protocol.
func TestConnectWithoutDataControl(t *testing.T) {
	path := startFakeCompositor(t, "wl_seat")
	if _, err := NewWayland(WaylandOptions{Display: path}); !errors.Is(err, ErrNoDataControl) {
		t.Fatalf("NewWayland = %v, want %v", err, ErrNoDataControl)
	}
}

// TestConnectBindsDataControl checks the bind of both protocol flavors.
func TestConnectBindsDataControl(t *testing.T) {
	for _, iface := range []string{dataControlExt, dataControlWLR} {
		path := startFakeCompositor(t, "wl_seat", iface)
		clip, err := NewWayland(WaylandOptions{Display: path})
		if err != nil {
			t.Fatalf("%s: NewWayland: %v", iface, err)
		}
		clip.state.connMu.Lock()
		conn := clip.state.conn
		clip.state.connMu.Unlock()
		if conn == nil {
			t.Fatalf("%s: no connection", iface)
		}
		if conn.protocol != iface {
			t.Fatalf("%s: bound %q", iface, conn.protocol)
		}
		if conn.device == nil {
			t.Fatalf("%s: no device", iface)
		}
		if err := clip.Close(); err != nil {
			t.Fatalf("%s: close: %v", iface, err)
		}
	}
}

// TestConnectWithoutSeat checks the error of a compositor without a seat.
func TestConnectWithoutSeat(t *testing.T) {
	path := startFakeCompositor(t, dataControlExt)
	if _, err := NewWayland(WaylandOptions{Display: path}); !errors.Is(err, errNoSeat) {
		t.Fatalf("NewWayland = %v, want %v", err, errNoSeat)
	}
}

// TestNewWaylandOptions checks the validation of the options.
func TestNewWaylandOptions(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := newWayland(WaylandOptions{}); !errors.Is(err, ErrNoWaylandDisplay) {
		t.Fatalf("empty options = %v, want %v", err, ErrNoWaylandDisplay)
	}
	if _, err := newWayland(WaylandOptions{Display: "wayland-0"}); !errors.Is(err, ErrNoWaylandDisplay) {
		t.Fatalf("no runtime dir = %v, want %v", err, ErrNoWaylandDisplay)
	}
	clip, err := newWayland(WaylandOptions{Display: "wayland-1", RuntimeDir: "/run/user/1000"})
	if err != nil {
		t.Fatalf("absolute runtime dir: %v", err)
	}
	if got := clip.socketPath(); got != "/run/user/1000/wayland-1" {
		t.Fatalf("socket path is %q", got)
	}
	if clip.valueTimeout != DefaultValueTimeout {
		t.Fatalf("value timeout is %v", clip.valueTimeout)
	}
	if _, err := newWayland(WaylandOptions{Display: "/run/user/1000/wayland-1", ValueTimeout: -time.Second}); err == nil {
		t.Fatal("a negative timeout has no error")
	}
	clip, err = newWayland(WaylandOptions{Display: "/tmp/wayland-0", ValueTimeout: time.Minute})
	if err != nil {
		t.Fatalf("absolute display: %v", err)
	}
	if clip.socketPath() != "/tmp/wayland-0" || clip.valueTimeout != time.Minute {
		t.Fatalf("socket path is %q, timeout %v", clip.socketPath(), clip.valueTimeout)
	}
}

// TestNewWaylandNoSocket checks the error of a session without a compositor.
func TestNewWaylandNoSocket(t *testing.T) {
	_, err := NewWayland(WaylandOptions{Display: "wayland-0", RuntimeDir: t.TempDir()})
	if !errors.Is(err, ErrNoWaylandDisplay) {
		t.Fatalf("NewWayland = %v, want %v", err, ErrNoWaylandDisplay)
	}
}

// TestConnectSilentCompositor checks that a compositor which accepts the
// connection but never answers does not block the handshake.
func TestConnectSilentCompositor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayland-silent")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Hold the connection open and answer nothing.
		_, _ = io.Copy(io.Discard, conn)
	}()
	previous := handshakeTimeout
	handshakeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { handshakeTimeout = previous })

	started := time.Now()
	if _, err := NewWayland(WaylandOptions{Display: path}); !errors.Is(err, errHandshakeTimeout) {
		t.Fatalf("NewWayland = %v, want %v", err, errHandshakeTimeout)
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Fatalf("the handshake waited %s", waited)
	}
}

// TestDeviceSelectionEvent checks the selection event with an announced offer.
func TestDeviceSelectionEvent(t *testing.T) {
	ctx := newTestContext(t)
	device := &dcDevice{}
	ctx.Register(device)
	offer := &dcOffer{}
	ctx.RegisterServer(offer, offerID)

	var got *dcOffer
	calls := 0
	device.setSelectionHandler(func(value *dcOffer) {
		calls++
		got = value
	})
	device.Dispatch(1, -1, readFragment(t, "dc_device_selection_offer.bin"))
	if calls != 1 || got != offer {
		t.Fatalf("selection handler calls = %d, offer = %v", calls, got)
	}

	device.Dispatch(1, -1, readFragment(t, "dc_device_selection_null.bin"))
	if calls != 2 || got != nil {
		t.Fatalf("a cleared selection is %v after %d calls", got, calls)
	}

	device.Dispatch(1, -1, u32Arg(0xdead))
	if calls != 3 || got != nil {
		t.Fatalf("an unknown offer is %v after %d calls", got, calls)
	}

	device.Dispatch(1, -1, []byte{1})
	if calls != 3 {
		t.Fatalf("a truncated event reached the handler, %d calls", calls)
	}
}

// TestDeviceDataOfferEvent checks the announcement of an offer and its MIME
// types.
func TestDeviceDataOfferEvent(t *testing.T) {
	ctx := newTestContext(t)
	device := &dcDevice{}
	ctx.Register(device)

	var announced *dcOffer
	device.setOfferHandler(func(value *dcOffer) { announced = value })
	device.Dispatch(0, -1, readFragment(t, "dc_device_data_offer.bin"))
	if announced == nil {
		t.Fatal("the offer was not announced")
	}
	if announced.ID() != offerID+1 {
		t.Fatalf("the offer id is %d", announced.ID())
	}
	announced.Dispatch(0, -1, readFragment(t, "dc_offer_text_plain.bin"))
	announced.Dispatch(0, -1, readFragment(t, "dc_offer_text_html.bin"))
	want := targetList{"text/plain", "text/html"}
	if got := announced.targets(); !reflect.DeepEqual(got, want) {
		t.Fatalf("targets are %v, want %v", got, want)
	}
	// A truncated body and an unknown opcode do not change the list.
	announced.Dispatch(0, -1, []byte{0xff})
	announced.Dispatch(7, -1, nil)
	if got := announced.targets(); !reflect.DeepEqual(got, want) {
		t.Fatalf("targets after bad events are %v, want %v", got, want)
	}
}

// TestDeviceFinishedEvent checks the loss of the device.
func TestDeviceFinishedEvent(t *testing.T) {
	ctx := newTestContext(t)
	device := &dcDevice{}
	ctx.Register(device)
	finished := false
	device.setFinishedHandler(func() { finished = true })
	device.Dispatch(2, -1, nil)
	if !finished {
		t.Fatal("the finished handler did not run")
	}
}

// TestSourceEvents checks the send and cancelled events.
func TestSourceEvents(t *testing.T) {
	ctx := newTestContext(t)
	source := &dcSource{}
	ctx.Register(source)

	var mime string
	sent := -1
	source.setSendHandler(func(value string, fd int) {
		mime, sent = value, fd
	})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()
	source.Dispatch(0, int(writer.Fd()), readFragment(t, "dc_source_send_text_plain.bin"))
	if mime != "text/plain" || sent != int(writer.Fd()) {
		t.Fatalf("send is %q on %d", mime, sent)
	}
	if err := os.NewFile(uintptr(sent), "test").Close(); err != nil {
		t.Fatalf("close the served descriptor: %v", err)
	}
	if !source.TakesFD(0) || source.TakesFD(1) {
		t.Fatal("wrong descriptor opcodes")
	}

	cancelled := false
	source.setCancelledHandler(func() { cancelled = true })
	source.Dispatch(1, -1, nil)
	if !cancelled {
		t.Fatal("the cancelled handler did not run")
	}
}

// TestSourceIgnoresUnwantedFD checks that an unused descriptor is closed.
func TestSourceIgnoresUnwantedFD(t *testing.T) {
	ctx := newTestContext(t)
	source := &dcSource{}
	ctx.Register(source)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()
	source.Dispatch(0, int(writer.Fd()), readFragment(t, "dc_source_send_text_plain.bin"))
	// The descriptor is closed now, so a read reports an end of file.
	if _, err := writer.Write([]byte("x")); err == nil {
		t.Fatal("the unused descriptor is still open")
	}
}

// TestStringAt checks the decoding of string arguments.
func TestStringAt(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string
	}{
		{"text", readFragment(t, "dc_offer_text_plain.bin"), "text/plain"},
		{"empty", u32Arg(0), ""},
		{"truncated length", []byte{1, 0}, ""},
		{"truncated value", []byte{9, 0, 0, 0, 'a'}, ""},
		{"missing terminator", []byte{4, 0, 0, 0, 'a', 'b', 'c', 'd'}, ""},
		{"offset", append(u32Arg(1), readFragment(t, "dc_offer_text_html.bin")...), "text/html"},
	}
	for _, test := range cases {
		if got := stringAt(test.body, 0); test.name != "offset" && got != test.want {
			t.Fatalf("%s: %q", test.name, got)
		}
	}
	if got := stringAt(cases[5].body, 4); got != "text/html" {
		t.Fatalf("offset: %q", got)
	}
}

// TestRequestMessage checks the header and the body of a request.
func TestRequestMessage(t *testing.T) {
	proxy := &dcOffer{}
	proxy.SetID(7)
	message := requestMessage(proxy, 3, u32Arg(9))
	if len(message) != 12 {
		t.Fatalf("message is %d bytes", len(message))
	}
	if got := binary.LittleEndian.Uint32(message[0:4]); got != 7 {
		t.Fatalf("object id is %d", got)
	}
	if got := binary.LittleEndian.Uint32(message[4:8]); got != 12<<16|3 {
		t.Fatalf("header is %#x", got)
	}
	if got := binary.LittleEndian.Uint32(message[8:12]); got != 9 {
		t.Fatalf("argument is %d", got)
	}
}

// TestPickManager checks the protocol preference.
func TestPickManager(t *testing.T) {
	globals := []client.RegistryGlobalEvent{
		{Name: 1, Interface: "wl_seat", Version: 9},
		{Name: 2, Interface: dataControlWLR, Version: 2},
	}
	global, ok := pickManager(globals)
	if !ok || global.Interface != dataControlWLR {
		t.Fatalf("manager is %v %v", global, ok)
	}
	if got := managerVersion(global); got != 2 {
		t.Fatalf("wlroots version is %d", got)
	}
	globals = append(globals, client.RegistryGlobalEvent{Name: 3, Interface: dataControlExt, Version: 1})
	global, ok = pickManager(globals)
	if !ok || global.Interface != dataControlExt {
		t.Fatalf("manager is %v %v", global, ok)
	}
	if got := managerVersion(global); got != 1 {
		t.Fatalf("staging version is %d", got)
	}
	if _, ok := pickManager(nil); ok {
		t.Fatal("a compositor without data-control has a manager")
	}
	seat, ok := pickSeat(globals)
	if !ok || seat.Name != 1 {
		t.Fatalf("seat is %v %v", seat, ok)
	}
	if _, ok := pickSeat(nil); ok {
		t.Fatal("a compositor without a seat has one")
	}
}

// TestPayloadValues checks the MIME types the clipboard offers.
func TestPayloadValues(t *testing.T) {
	payload, err := payloadOf(ports.Content{
		ItemID: "item-1",
		Representations: []ports.Representation{
			{MIME: MIMETextPlain, Data: []byte("hello")},
			{MIME: MIMETextHTML, Data: []byte("<b>hello</b>")},
			{MIME: MIMEImagePNG, Data: encodeTestImage(t, MIMEImagePNG)},
		},
	})
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	values := payload.values()
	want := []string{MarkerTarget, waylandTextUTF8, MIMETextPlain, waylandTextLegacy, MIMETextHTML, MIMEImagePNG}
	if got := mimesOf(values); !reflect.DeepEqual(got, want) {
		t.Fatalf("MIME types are %v, want %v", got, want)
	}
	for _, value := range values {
		if value.mime == MarkerTarget && string(value.data) != "item-1" {
			t.Fatalf("the marker is %q", value.data)
		}
	}

	plain, err := payloadOf(ports.Content{Representations: []ports.Representation{{MIME: MIMETextPlain, Data: []byte("x")}}})
	if err != nil {
		t.Fatalf("plain payload: %v", err)
	}
	if values := plain.values(); len(values) != 4 {
		t.Fatalf("a text payload has %d values", len(values))
	}
}

// TestPayloadValuesFiles checks the file list of the served content.
func TestPayloadValuesFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.txt")
	second := filepath.Join(dir, "two.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	payload, err := payloadOf(ports.Content{Representations: []ports.Representation{
		{MIME: MIMEUnisonFile, FilePath: first},
		{MIME: MIMEUnisonFile, FilePath: second},
	}})
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	values := payload.values()
	want := []string{MarkerTarget, waylandTextUTF8, MIMETextPlain, waylandTextLegacy, MIMEGNOMECopiedFiles, MIMEURIList}
	if got := mimesOf(values); !reflect.DeepEqual(got, want) {
		t.Fatalf("MIME types are %v, want %v", got, want)
	}
	for _, value := range values {
		switch value.mime {
		case waylandTextUTF8:
			if string(value.data) != first+"\n"+second+"\n" {
				t.Fatalf("plain paths are %q", value.data)
			}
		case MIMEGNOMECopiedFiles:
			if got := parseGNOMECopiedFiles(value.data); !reflect.DeepEqual(got, []string{pathToURI(first), pathToURI(second)}) {
				t.Fatalf("gnome list is %v", got)
			}
		case MIMEURIList:
			if got := parseURIList(value.data); !reflect.DeepEqual(got, []string{pathToURI(first), pathToURI(second)}) {
				t.Fatalf("uri list is %v", got)
			}
		}
	}
}

// TestPipeSource checks the reading of a transfer pipe.
func TestPipeSource(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()
	go func() {
		_, _ = writer.Write([]byte("hello"))
		_ = writer.Close()
	}()
	value, err := readValue(context.Background(), pipeSource{file: reader, timeout: time.Second}, MaxValueBytes)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(value) != "hello" {
		t.Fatalf("value is %q", value)
	}
}

// TestPipeSourceLimit checks the size limit of a value.
func TestPipeSourceLimit(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()
	go func() {
		_, _ = writer.Write([]byte("0123456789"))
		_ = writer.Close()
	}()
	if _, err := readValue(context.Background(), pipeSource{file: reader, timeout: time.Second}, 4); !errors.Is(err, errValueTooLarge) {
		t.Fatalf("read = %v, want %v", err, errValueTooLarge)
	}
}

// TestPipeSourceTimeout checks a value that never ends.
func TestPipeSourceTimeout(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	_, err = readValue(context.Background(), pipeSource{file: reader, timeout: 50 * time.Millisecond}, MaxValueBytes)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read = %v, want a deadline", err)
	}
}

// mimesOf returns the MIME types of the values in order.
func mimesOf(values []waylandValue) []string {
	mimes := make([]string, 0, len(values))
	for _, value := range values {
		mimes = append(mimes, value.mime)
	}
	return mimes
}
