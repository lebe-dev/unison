//go:build linux

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpinningVinyl/go-wayland/wayland/client"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	// waylandMinReconnectDelay and waylandMaxReconnectDelay bound the wait
	// between attempts to reach the compositor.
	waylandMinReconnectDelay = time.Second
	waylandMaxReconnectDelay = 30 * time.Second

	// waylandConnectWait bounds how long Write and Read wait for a session.
	waylandConnectWait = 5 * time.Second

	// waylandRoundTripTimeout bounds the wait for the compositor to process
	// the requests sent so far.
	waylandRoundTripTimeout = time.Second

	// waylandPipeChunk is the largest part of a value read from one pipe at
	// once.
	waylandPipeChunk = 64 << 10

	// waylandTextUTF8 is the plain text MIME type every Wayland client
	// understands.
	waylandTextUTF8 = "text/plain;charset=utf-8"

	// waylandTextLegacy is the plain text name an X11 client behind XWayland
	// asks for.
	waylandTextLegacy = "UTF8_STRING"
)

var (
	// errNoSeat reports a compositor without a seat.
	errNoSeat = errors.New("clipboard: the compositor has no seat")
	// errConnClosed reports a protocol connection that is gone.
	errConnClosed = errors.New("clipboard: the Wayland connection is closed")
)

// WaylandOptions configures a clipboard of the data-control protocol.
type WaylandOptions struct {
	// Display is the Wayland socket name in the WAYLAND_DISPLAY format. The
	// environment variable is used when empty.
	Display string
	// RuntimeDir is the directory of the socket. XDG_RUNTIME_DIR is used when
	// empty.
	RuntimeDir string
	// ValueTimeout bounds one part of a value read. Defaults to
	// DefaultValueTimeout.
	ValueTimeout time.Duration
	// Logger receives connection and transfer events. No content is logged.
	Logger *slog.Logger
	// OnReport receives every observed clipboard state with the MIME types the
	// owner advertised. It runs on the watcher goroutine and must not block.
	OnReport func(Report)
}

// Wayland is a ports.Clipboard backed by the data-control protocol. The
// protocol lets a client outside the seat focus read and set the selection,
// which KDE Plasma, wlroots compositors and COSMIC offer. Like the X11
// backend, only CLIPBOARD is synchronized and content tagged with the password
// manager hint is reported as sensitive and never read.
//
// The clipboard connects when it is created, so a session without the protocol
// is reported at once and the daemon can pick another backend.
type Wayland struct {
	display      string
	runtimeDir   string
	valueTimeout time.Duration
	logger       *slog.Logger
	onReport     func(Report)
	state        *waylandState
}

var _ ports.Clipboard = (*Wayland)(nil)

// waylandState is the runtime state of the clipboard.
type waylandState struct {
	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc

	connMu sync.Mutex
	conn   *waylandConn

	subMu sync.Mutex
	subs  map[chan ports.Snapshot]struct{}
}

// newWayland validates the options and builds the configuration.
func newWayland(opts WaylandOptions) (*Wayland, error) {
	display := strings.TrimSpace(opts.Display)
	if display == "" {
		display = strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY"))
	}
	runtimeDir := strings.TrimSpace(opts.RuntimeDir)
	if runtimeDir == "" {
		runtimeDir = strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	}
	if display == "" || (runtimeDir == "" && !filepath.IsAbs(display)) {
		return nil, ErrNoWaylandDisplay
	}
	if opts.ValueTimeout < 0 {
		return nil, errors.New("clipboard: negative timeout")
	}
	valueTimeout := opts.ValueTimeout
	if valueTimeout == 0 {
		valueTimeout = DefaultValueTimeout
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Wayland{
		display:      display,
		runtimeDir:   runtimeDir,
		valueTimeout: valueTimeout,
		logger:       logger,
		onReport:     opts.OnReport,
		state:        &waylandState{subs: make(map[chan ports.Snapshot]struct{})},
	}, nil
}

// NewWayland returns a clipboard that talks to the compositor of the session.
// It reports ErrNoDataControl when the compositor does not offer a data-control
// protocol, which is what a GNOME session does.
func NewWayland(opts WaylandOptions) (*Wayland, error) {
	w, err := newWayland(opts)
	if err != nil {
		return nil, err
	}
	conn, err := connectWayland(w)
	if err != nil {
		return nil, err
	}
	w.state.conn = conn
	w.start()
	return w, nil
}

// Close stops the watcher and releases the protocol connection.
func (w *Wayland) Close() error {
	w.state.mu.Lock()
	cancel := w.state.cancel
	w.state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.state.connMu.Lock()
	conn := w.state.conn
	w.state.conn = nil
	w.state.connMu.Unlock()
	if conn != nil {
		conn.close()
	}
	return nil
}

// Watch delivers a snapshot for every change of the CLIPBOARD selection. The
// content present at the start of watching is not reported.
func (w *Wayland) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make(chan ports.Snapshot, watchBuffer)
	w.state.subMu.Lock()
	w.state.subs[out] = struct{}{}
	w.state.subMu.Unlock()
	go func() {
		<-ctx.Done()
		w.state.subMu.Lock()
		if _, ok := w.state.subs[out]; ok {
			delete(w.state.subs, out)
			close(out)
		}
		w.state.subMu.Unlock()
	}()
	return out, nil
}

// Write takes the CLIPBOARD selection and serves the content until another
// client takes it over.
func (w *Wayland) Write(ctx context.Context, content ports.Content) error {
	prepared, err := payloadOf(content)
	if err != nil {
		return err
	}
	conn, err := w.awaitConn(ctx)
	if err != nil {
		return fmt.Errorf("clipboard: write: %w", err)
	}
	if err := conn.own(ctx, &prepared); err != nil {
		return fmt.Errorf("clipboard: write: %w", err)
	}
	w.logger.Info("took ownership of the clipboard",
		"plain_bytes", len(prepared.plain),
		"html_bytes", len(prepared.html),
		"png_bytes", len(prepared.png),
		"files", len(prepared.files),
		"has_item_id", prepared.itemID != "",
	)
	return nil
}

// Read returns the state of the clipboard now. The report is empty when the
// clipboard has no owner or advertises nothing the backend reads.
func (w *Wayland) Read(ctx context.Context) (Report, error) {
	conn, err := w.awaitConn(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("clipboard: read: %w", err)
	}
	// The selection arrives as an event, so a round trip is what makes the
	// state current for a write that happened after the connection started.
	wait, cancel := context.WithTimeout(ctx, waylandRoundTripTimeout)
	err = conn.roundTrip(wait)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return Report{}, fmt.Errorf("clipboard: read: %w", ctx.Err())
		}
		w.logger.Debug("could not wait for the clipboard state", "error", err)
	}
	conn.readMu.Lock()
	// A change that arrives during this read is published afterwards; one that
	// arrived before is covered by the state read here.
	conn.pending.Store(false)
	offer, targets := conn.selection()
	report, _ := conn.read(offer, targets)
	conn.retire()
	conn.readMu.Unlock()
	if conn.pending.Swap(false) {
		go conn.readSnapshot()
	}
	return report, nil
}

// socketPath returns the path of the compositor socket.
func (w *Wayland) socketPath() string {
	if filepath.IsAbs(w.display) {
		return w.display
	}
	return filepath.Join(w.runtimeDir, w.display)
}

// start launches the supervisor of the protocol connection once.
func (w *Wayland) start() {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if w.state.started {
		return
	}
	w.state.started = true
	ctx, cancel := context.WithCancel(context.Background())
	w.state.cancel = cancel
	go w.supervise(ctx)
}

// supervise keeps a protocol connection and restores it after the compositor
// restarts.
func (w *Wayland) supervise(ctx context.Context) {
	delay := waylandMinReconnectDelay
	for {
		conn := w.takeConn()
		if conn == nil {
			connected, err := connectWayland(w)
			if err != nil {
				w.logger.Debug("the Wayland clipboard is not reachable", "error", err)
				if !sleep(ctx, delay) {
					return
				}
				delay = min(2*delay, waylandMaxReconnectDelay)
				continue
			}
			conn = connected
			w.logger.Info("connected to the Wayland clipboard",
				"display", w.display, "protocol", conn.protocol)
		}
		w.setConn(conn)
		delay = waylandMinReconnectDelay
		conn.serve(ctx)
		w.dropConn(conn)
		if ctx.Err() != nil {
			return
		}
		w.logger.Warn("lost the Wayland clipboard connection, reconnecting")
		if !sleep(ctx, delay) {
			return
		}
		delay = min(2*delay, waylandMaxReconnectDelay)
	}
}

// awaitConn returns the live connection, waiting for the supervisor.
func (w *Wayland) awaitConn(ctx context.Context) (*waylandConn, error) {
	deadline := time.Now().Add(waylandConnectWait)
	for {
		w.state.connMu.Lock()
		conn := w.state.conn
		w.state.connMu.Unlock()
		if conn != nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, errConnClosed
		}
		if !sleep(ctx, 20*time.Millisecond) {
			return nil, ctx.Err()
		}
	}
}

// takeConn returns the connection of the constructor and forgets it.
func (w *Wayland) takeConn() *waylandConn {
	w.state.connMu.Lock()
	defer w.state.connMu.Unlock()
	conn := w.state.conn
	w.state.conn = nil
	return conn
}

// setConn publishes the connection of the supervisor.
func (w *Wayland) setConn(conn *waylandConn) {
	w.state.connMu.Lock()
	w.state.conn = conn
	w.state.connMu.Unlock()
}

// dropConn forgets the connection after it failed.
func (w *Wayland) dropConn(conn *waylandConn) {
	w.state.connMu.Lock()
	if w.state.conn == conn {
		w.state.conn = nil
	}
	w.state.connMu.Unlock()
}

// publish delivers a snapshot to every watcher. A watcher that cannot keep up
// loses the snapshot.
func (w *Wayland) publish(snapshot ports.Snapshot) {
	w.state.subMu.Lock()
	defer w.state.subMu.Unlock()
	for ch := range w.state.subs {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

// waylandConn is one protocol connection with its objects and event loop.
type waylandConn struct {
	w        *Wayland
	display  *client.Display
	manager  *dcManager
	device   *dcDevice
	protocol string

	// dispatches carries decoded events from the reader to the event loop,
	// commands carry protocol work from other goroutines.
	dispatches chan func() error
	commands   chan func()
	done       chan struct{}
	closeOnce  sync.Once

	mu       sync.Mutex
	offer    *dcOffer
	targets  targetList
	retired  []*dcOffer
	source   *dcSource
	baseline bool

	readMu  sync.Mutex
	pending atomic.Bool
}

// connectWayland dials the compositor, binds the objects and reads the starting
// state of the clipboard.
func connectWayland(w *Wayland) (*waylandConn, error) {
	display, err := client.Connect(w.socketPath())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoWaylandDisplay, err)
	}
	conn := &waylandConn{
		w:          w,
		display:    display,
		dispatches: make(chan func() error),
		commands:   make(chan func()),
		done:       make(chan struct{}),
		baseline:   true,
	}
	ok := false
	defer func() {
		if !ok {
			conn.close()
		}
	}()
	if err := conn.bind(); err != nil {
		return nil, err
	}
	// The device reports the selection right after the bind. That content was
	// there before the clipboard started, so it is the starting state and not a
	// change to report.
	if err := conn.drain(); err != nil {
		return nil, fmt.Errorf("clipboard: read the starting selection: %w", err)
	}
	conn.mu.Lock()
	conn.baseline = false
	conn.mu.Unlock()
	ok = true
	return conn, nil
}

// bind finds the data-control manager and the seat and creates the device.
func (c *waylandConn) bind() error {
	registry, err := c.display.GetRegistry()
	if err != nil {
		return fmt.Errorf("clipboard: registry: %w", err)
	}
	var globals []client.RegistryGlobalEvent
	registry.SetGlobalHandler(func(event client.RegistryGlobalEvent) {
		globals = append(globals, event)
	})
	if err := displayRoundTrip(c.display); err != nil {
		return fmt.Errorf("clipboard: list the globals: %w", err)
	}
	managerGlobal, ok := pickManager(globals)
	if !ok {
		return ErrNoDataControl
	}
	seatGlobal, ok := pickSeat(globals)
	if !ok {
		return errNoSeat
	}
	manager := newDCManager(c.display.Context())
	if err := registry.Bind(managerGlobal.Name, managerGlobal.Interface, managerVersion(managerGlobal), manager); err != nil {
		return fmt.Errorf("clipboard: bind the data-control manager: %w", err)
	}
	seat := client.NewSeat(c.display.Context())
	if err := registry.Bind(seatGlobal.Name, seatGlobal.Interface, min(seatGlobal.Version, 1), seat); err != nil {
		return fmt.Errorf("clipboard: bind the seat: %w", err)
	}
	device, err := manager.getDataDevice(seat)
	if err != nil {
		return fmt.Errorf("clipboard: create the data-control device: %w", err)
	}
	device.setSelectionHandler(c.selectionChanged)
	device.setFinishedHandler(func() {
		c.logger().Warn("the compositor finished the clipboard device")
		c.close()
	})
	c.manager, c.device, c.protocol = manager, device, managerGlobal.Interface
	return nil
}

// drain processes the events of the starting selection.
func (c *waylandConn) drain() error {
	return displayRoundTrip(c.display)
}

// serve runs the event loop until the connection fails or the context ends.
func (c *waylandConn) serve(ctx context.Context) {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			c.close()
		case <-stop:
		}
	}()
	go c.readLoop(stop)
	for {
		select {
		case <-ctx.Done():
			c.close()
			return
		case fn := <-c.dispatches:
			if err := fn(); err != nil {
				if ctx.Err() == nil {
					c.logger().Warn("the Wayland clipboard connection failed", "error", err)
				}
				c.close()
				return
			}
		case command := <-c.commands:
			command()
		}
	}
}

// readLoop decodes incoming messages and hands them to the event loop.
func (c *waylandConn) readLoop(stop <-chan struct{}) {
	for {
		fn := c.display.Context().GetDispatch()
		select {
		case c.dispatches <- fn:
		case <-c.done:
			return
		case <-stop:
			return
		}
	}
}

// close releases the connection once.
func (c *waylandConn) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.display.Context().Close()
	})
}

// roundTrip waits until the compositor processed every request sent so far and
// every event it sent before was dispatched. The sync callback is created on
// the event loop, and its done event closes the channel from there.
func (c *waylandConn) roundTrip(ctx context.Context) error {
	done := make(chan struct{})
	var once sync.Once
	err := c.do(ctx, func() error {
		callback, err := c.display.Sync()
		if err != nil {
			return err
		}
		callback.SetDoneHandler(func(client.CallbackDoneEvent) {
			once.Do(func() { close(done) })
			_ = callback.Destroy()
		})
		return nil
	})
	if err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	case <-c.done:
		return errConnClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// do runs protocol work on the event loop and waits for it.
func (c *waylandConn) do(ctx context.Context, fn func() error) error {
	result := make(chan error, 1)
	select {
	case c.commands <- func() { result <- fn() }:
	case <-c.done:
		return errConnClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-result:
		return err
	case <-c.done:
		return errConnClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// logger returns the logger of the clipboard.
func (c *waylandConn) logger() *slog.Logger { return c.w.logger }

// selectionChanged records the new selection and starts reading it. A selection
// that arrives while the connection is starting describes the content that was
// there before it, so it is the starting state and not a change.
func (c *waylandConn) selectionChanged(offer *dcOffer) {
	c.mu.Lock()
	previous := c.offer
	c.offer = offer
	if offer != nil {
		c.targets = offer.targets()
	} else {
		c.targets = nil
	}
	if previous != nil && previous != offer {
		c.retired = append(c.retired, previous)
	}
	baseline := c.baseline
	c.mu.Unlock()
	if baseline {
		return
	}
	go c.readSnapshot()
}

// selection returns the current offer and its MIME types.
func (c *waylandConn) selection() (*dcOffer, targetList) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offer, append(targetList(nil), c.targets...)
}

// retire destroys the offers of earlier selections after their state was read.
func (c *waylandConn) retire() {
	c.mu.Lock()
	pending := len(c.retired) > 0
	c.mu.Unlock()
	if !pending {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.w.valueTimeout)
	defer cancel()
	_ = c.do(ctx, func() error {
		c.mu.Lock()
		offers := c.retired
		c.retired = nil
		current := c.offer
		c.mu.Unlock()
		for _, offer := range offers {
			if offer == nil || offer == current {
				continue
			}
			_ = offer.destroy()
		}
		return nil
	})
}

// readSnapshot reads the current selection and publishes it. A change that
// arrives during a read is read again after it.
func (c *waylandConn) readSnapshot() {
	if !c.readMu.TryLock() {
		c.pending.Store(true)
		return
	}
	defer c.readMu.Unlock()
	for {
		c.publishCurrent()
		if !c.pending.Swap(false) {
			return
		}
	}
}

// publishCurrent reads the selection and reports it to the watchers.
func (c *waylandConn) publishCurrent() {
	offer, targets := c.selection()
	report, ok := c.read(offer, targets)
	c.retire()
	if !ok {
		return
	}
	c.w.publish(report.Snapshot)
	if c.w.onReport != nil {
		c.w.onReport(report)
	}
}

// read returns the state of the selection. It reports false when the clipboard
// has nothing the backend reads, but the report still carries the MIME types.
func (c *waylandConn) read(offer *dcOffer, targets targetList) (Report, bool) {
	report := Report{At: time.Now(), Targets: targets}
	if offer == nil {
		return report, false
	}
	report.Owner = uint64(offer.ID())
	owned := targets.has(MarkerTarget)
	report.OwnerIsUs = owned
	if Sensitive(targets) {
		c.logger().Info("skipped a sensitive clipboard value", "targets", len(targets))
		report.Snapshot = ports.Snapshot{Sensitive: true, OwnedByUs: owned}
		return report, true
	}
	representations := make([]ports.Representation, 0, 3+maxFiles)
	if target, _ := targets.pickPlain(); target != "" {
		if value, ok := c.readValue(offer, target); ok && len(value) > 0 {
			representations = append(representations, ports.Representation{
				MIME: MIMETextPlain,
				Data: value,
			})
		}
	}
	if target := targets.pickHTML(); target != "" {
		if value, ok := c.readValue(offer, target); ok && len(value) > 0 {
			representations = append(representations, ports.Representation{
				MIME: MIMETextHTML,
				Data: decodeHTML(value),
			})
		}
	}
	imageBytes := 0
	if target := targets.pickImage(); target != "" {
		if value, ok := c.readValue(offer, target); ok && len(value) > 0 {
			png, err := readImagePNG(target, value)
			switch {
			case err != nil:
				c.logger().Debug("skipped a clipboard image", "mime", target, "bytes", len(value), "error", err)
			case len(png) > 0:
				imageBytes = len(png)
				representations = append(representations, ports.Representation{MIME: MIMEImagePNG, Data: png})
			}
		}
	}
	fileList := []string(nil)
	if target := targets.pickFiles(); target != "" {
		if value, ok := c.readValue(offer, target); ok && len(value) > 0 {
			fileList = parseFileList(target, value)
		}
	}
	for _, path := range fileList {
		representations = append(representations, ports.Representation{MIME: MIMEUnisonFile, FilePath: path})
	}
	report.Snapshot = ports.Snapshot{
		Content:   ports.Content{Representations: representations},
		OwnedByUs: owned,
	}
	if len(representations) == 0 {
		return report, false
	}
	plainBytes, htmlBytes := 0, 0
	for _, representation := range representations {
		switch representation.MIME {
		case MIMETextPlain:
			plainBytes = len(representation.Data)
		case MIMETextHTML:
			htmlBytes = len(representation.Data)
		}
	}
	c.logger().Info("read the clipboard", "plain_bytes", plainBytes, "html_bytes", htmlBytes,
		"png_bytes", imageBytes, "files", len(fileList), "owned_by_us", owned)
	return report, true
}

// readValue asks the owner for one MIME type and reads the pipe of the
// transfer.
func (c *waylandConn) readValue(offer *dcOffer, mime string) ([]byte, bool) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.w.valueTimeout)
	err = c.do(ctx, func() error { return offer.receive(mime, int(writer.Fd())) })
	cancel()
	// The compositor holds its own descriptor now, so the local one only keeps
	// the pipe from reaching EOF.
	_ = writer.Close()
	if err != nil {
		_ = reader.Close()
		c.logger().Debug("could not request a clipboard value", "mime", mime, "error", err)
		return nil, false
	}
	defer func() { _ = reader.Close() }()
	value, err := readValue(context.Background(), pipeSource{file: reader, timeout: c.w.valueTimeout}, MaxValueBytes)
	if err != nil {
		c.logger().Debug("could not read a clipboard value", "mime", mime, "error", err)
		return nil, false
	}
	return value, true
}

// pipeSource reads one clipboard value from the pipe of a transfer.
type pipeSource struct {
	file    *os.File
	timeout time.Duration
}

// next returns the next part of the value.
func (s pipeSource) next(context.Context) ([]byte, bool, error) {
	buf := make([]byte, waylandPipeChunk)
	for {
		if err := s.file.SetReadDeadline(time.Now().Add(s.timeout)); err != nil {
			return nil, false, err
		}
		n, err := s.file.Read(buf)
		if n > 0 {
			return buf[:n], true, nil
		}
		switch {
		case errors.Is(err, io.EOF):
			return nil, false, nil
		case err != nil:
			return nil, false, err
		}
	}
}

// own takes the selection with the prepared content.
func (c *waylandConn) own(ctx context.Context, content *payload) error {
	values := content.values()
	return c.do(ctx, func() error {
		if c.device == nil {
			return errConnClosed
		}
		source, err := c.manager.createDataSource()
		if err != nil {
			return err
		}
		for _, value := range values {
			if err := source.offer(value.mime); err != nil {
				_ = source.destroy()
				return err
			}
		}
		source.setSendHandler(func(mime string, fd int) {
			go serveValue(values, mime, fd)
		})
		source.setCancelledHandler(func() { c.lostOwnership(source) })
		if err := c.device.setSelection(source); err != nil {
			_ = source.destroy()
			return err
		}
		c.mu.Lock()
		c.source = source
		c.mu.Unlock()
		return nil
	})
}

// lostOwnership drops the source after another client took the selection.
func (c *waylandConn) lostOwnership(source *dcSource) {
	c.mu.Lock()
	current := c.source
	if current == source {
		c.source = nil
	}
	c.mu.Unlock()
	if current == source {
		c.logger().Info("lost the clipboard selection")
	}
	_ = source.destroy()
}

// waylandValue is one MIME representation of the served content.
type waylandValue struct {
	mime string
	data []byte
}

// values lists the representations of the content in the MIME types the X11
// backend serves as well: plain text, HTML, PNG, a Nautilus file list, an
// RFC 2483 URI list and the marker with the clip item id.
func (p *payload) values() []waylandValue {
	values := []waylandValue{{mime: MarkerTarget, data: []byte(p.itemID)}}
	if plain := p.plainValue(); plain != nil {
		values = append(values,
			waylandValue{mime: waylandTextUTF8, data: plain},
			waylandValue{mime: MIMETextPlain, data: plain},
			waylandValue{mime: waylandTextLegacy, data: plain},
		)
	}
	if p.html != nil {
		values = append(values, waylandValue{mime: MIMETextHTML, data: p.html})
	}
	if p.png != nil {
		values = append(values, waylandValue{mime: MIMEImagePNG, data: p.png})
	}
	if len(p.files) > 0 {
		values = append(values,
			waylandValue{mime: MIMEGNOMECopiedFiles, data: encodeGNOMECopiedFiles(p.files)},
			waylandValue{mime: MIMEURIList, data: encodeURIList(p.files)},
		)
	}
	return values
}

// serveValue writes the requested representation into the pipe of a transfer
// and closes it.
func serveValue(values []waylandValue, mime string, fd int) {
	file := os.NewFile(uintptr(fd), "unison-clipboard-value")
	if file == nil {
		closeReceivedFD(fd)
		return
	}
	defer func() { _ = file.Close() }()
	for _, value := range values {
		if value.mime != mime {
			continue
		}
		writeAll(file, value.data)
		return
	}
}

// writeAll writes the value to the pipe until every byte is out.
func writeAll(file *os.File, data []byte) {
	for len(data) > 0 {
		n, err := file.Write(data)
		if err != nil {
			return
		}
		data = data[n:]
	}
}

// displayRoundTrip waits until the compositor processed every request sent so
// far.
func displayRoundTrip(display *client.Display) error {
	callback, err := display.Sync()
	if err != nil {
		return err
	}
	defer func() { _ = callback.Destroy() }()
	done := false
	callback.SetDoneHandler(func(client.CallbackDoneEvent) { done = true })
	for !done {
		if err := display.Context().Dispatch(); err != nil {
			return err
		}
	}
	return nil
}

// pickManager returns the manager global, the staging protocol first.
func pickManager(globals []client.RegistryGlobalEvent) (client.RegistryGlobalEvent, bool) {
	var fallback client.RegistryGlobalEvent
	found := false
	for _, global := range globals {
		switch global.Interface {
		case dataControlExt:
			return global, true
		case dataControlWLR:
			if !found {
				fallback, found = global, true
			}
		}
	}
	return fallback, found
}

// pickSeat returns the first seat global.
func pickSeat(globals []client.RegistryGlobalEvent) (client.RegistryGlobalEvent, bool) {
	for _, global := range globals {
		if global.Interface == "wl_seat" {
			return global, true
		}
	}
	return client.RegistryGlobalEvent{}, false
}

// managerVersion returns the version to bind: version 2 of the wlroots
// protocol adds the primary selection, the staging protocol is at version 1.
func managerVersion(global client.RegistryGlobalEvent) uint32 {
	if global.Interface == dataControlWLR {
		return min(global.Version, 2)
	}
	return min(global.Version, 1)
}
