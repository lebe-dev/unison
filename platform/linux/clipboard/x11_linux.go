//go:build linux

package clipboard

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	// minReconnectDelay and maxReconnectDelay bound the wait between attempts
	// to reach the X server.
	minReconnectDelay = time.Second
	maxReconnectDelay = 30 * time.Second

	// watchBuffer is the number of snapshots buffered for a watcher. A slow
	// watcher loses snapshots instead of blocking the connection.
	watchBuffer = 16

	// propertyChunk is the largest property value read at once.
	propertyChunk = 64 << 10

	// incrChunk is the size of one INCR chunk sent to a requestor.
	incrChunk = 64 << 10

	// maxTransferTime bounds one value read.
	maxTransferTime = 30 * time.Second

	// writeConnectTimeout bounds how long Write waits for the X server.
	writeConnectTimeout = 5 * time.Second

	// incrThreshold is the value size above which the selection is served
	// through the INCR protocol.
	incrThreshold = 64 << 10
)

// X11 is a ports.Clipboard backed by the X11 CLIPBOARD selection.
type X11 struct {
	display        string
	targetsTimeout time.Duration
	valueTimeout   time.Duration
	logger         *slog.Logger
	onReport       func(Report)
	state          *x11State
}

var _ ports.Clipboard = (*X11)(nil)

// newX11 validates the options and builds the configuration of a clipboard.
func newX11(opts X11Options) (*X11, error) {
	display := strings.TrimSpace(opts.Display)
	if display == "" {
		display = strings.TrimSpace(os.Getenv("DISPLAY"))
	}
	if display == "" {
		return nil, ErrNoDisplay
	}
	if opts.TargetsTimeout < 0 || opts.ValueTimeout < 0 {
		return nil, errors.New("clipboard: negative timeout")
	}
	targetsTimeout := opts.TargetsTimeout
	if targetsTimeout == 0 {
		targetsTimeout = DefaultTargetsTimeout
	}
	valueTimeout := opts.ValueTimeout
	if valueTimeout == 0 {
		valueTimeout = DefaultValueTimeout
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &X11{
		display:        display,
		targetsTimeout: targetsTimeout,
		valueTimeout:   valueTimeout,
		logger:         logger,
		onReport:       opts.OnReport,
	}, nil
}

// x11State is the runtime state of the clipboard connection.
type x11State struct {
	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc

	connectMu sync.Mutex
	connMu    sync.Mutex
	conn      *x11Conn

	serverMu sync.Mutex
	server   *server

	ownedMu sync.Mutex
	owned   *payload

	subMu sync.Mutex
	subs  map[chan ports.Snapshot]struct{}
}

// NewX11 returns a clipboard that talks to the X server of the display. It does
// not connect until the first watch or write, so a daemon starts with a display
// that appears later.
func NewX11(opts X11Options) (*X11, error) {
	x, err := newX11(opts)
	if err != nil {
		return nil, err
	}
	x.state = &x11State{subs: make(map[chan ports.Snapshot]struct{})}
	return x, nil
}

// Close stops the watcher and releases the X connection.
func (x *X11) Close() error {
	x.state.mu.Lock()
	cancel := x.state.cancel
	x.state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// Watch delivers a snapshot for every change of the CLIPBOARD owner. The
// content present at the start of watching is not reported.
func (x *X11) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	x.start()
	out := make(chan ports.Snapshot, watchBuffer)
	x.state.subMu.Lock()
	x.state.subs[out] = struct{}{}
	x.state.subMu.Unlock()
	go func() {
		<-ctx.Done()
		x.state.subMu.Lock()
		if _, ok := x.state.subs[out]; ok {
			delete(x.state.subs, out)
			close(out)
		}
		x.state.subMu.Unlock()
	}()
	return out, nil
}

// Write takes ownership of the CLIPBOARD selection and serves the content until
// another client takes it over.
func (x *X11) Write(ctx context.Context, content ports.Content) error {
	prepared, err := payloadOf(content)
	if err != nil {
		return err
	}
	conn, err := x.connection(ctx)
	if err != nil {
		return fmt.Errorf("clipboard: write: %w", err)
	}
	x.state.ownedMu.Lock()
	x.state.owned = &prepared
	x.state.ownedMu.Unlock()
	if err := own(conn, &prepared); err != nil {
		x.state.ownedMu.Lock()
		x.state.owned = nil
		x.state.ownedMu.Unlock()
		return fmt.Errorf("clipboard: write: %w", err)
	}
	x.logger.Info("took ownership of the clipboard",
		"plain_bytes", len(prepared.plain),
		"html_bytes", len(prepared.html),
		"png_bytes", len(prepared.png),
		"files", len(prepared.files),
		"has_item_id", prepared.itemID != "",
	)
	return nil
}

// start launches the supervisor of the X connection once.
func (x *X11) start() {
	x.state.mu.Lock()
	defer x.state.mu.Unlock()
	if x.state.started {
		return
	}
	x.state.started = true
	ctx, cancel := context.WithCancel(context.Background())
	x.state.cancel = cancel
	go x.supervise(ctx)
}

// connection returns a live X connection, waiting for a server that is still
// starting.
func (x *X11) connection(ctx context.Context) (*x11Conn, error) {
	x.start()
	ctx, cancel := context.WithTimeout(ctx, writeConnectTimeout)
	defer cancel()
	delay := 50 * time.Millisecond
	for {
		conn, err := x.ensureConnection()
		if err == nil {
			return conn, nil
		}
		if !sleep(ctx, delay) {
			return nil, fmt.Errorf("%w: %w", err, ctx.Err())
		}
		delay = min(2*delay, time.Second)
	}
}

// ensureConnection connects to the X server when there is no connection yet.
func (x *X11) ensureConnection() (*x11Conn, error) {
	x.state.connectMu.Lock()
	defer x.state.connectMu.Unlock()
	x.state.connMu.Lock()
	conn := x.state.conn
	x.state.connMu.Unlock()
	if conn != nil {
		return conn, nil
	}
	conn, err := dial(x.display)
	if err != nil {
		return nil, err
	}
	x.install(conn)
	return conn, nil
}

// install publishes a new connection and restores the ownership of content that
// survived a reconnect.
func (x *X11) install(conn *x11Conn) {
	x.state.connMu.Lock()
	x.state.conn = conn
	x.state.connMu.Unlock()
	x.logger.Info("connected to the X11 clipboard", "display", x.display)
	x.state.ownedMu.Lock()
	owned := x.state.owned
	x.state.ownedMu.Unlock()
	if owned == nil {
		return
	}
	if err := own(conn, owned); err != nil {
		x.logger.Warn("could not restore the clipboard selection", "error", err)
	}
}

// dropConn forgets the connection and closes it.
func (x *X11) dropConn(conn *x11Conn) {
	x.state.connMu.Lock()
	current := x.state.conn
	if conn != nil && current != conn {
		x.state.connMu.Unlock()
		return
	}
	x.state.conn = nil
	x.state.connMu.Unlock()
	if current != nil {
		current.conn.Close()
	}
}

// supervise keeps a connection to the X server and restores it after the server
// restarts.
func (x *X11) supervise(ctx context.Context) {
	delay := minReconnectDelay
	for {
		conn, err := x.ensureConnection()
		if err != nil {
			x.logger.Debug("the X11 clipboard is not reachable", "error", err)
			if !sleep(ctx, delay) {
				return
			}
			delay = min(2*delay, maxReconnectDelay)
			continue
		}
		delay = minReconnectDelay
		x.serve(ctx, conn)
		x.dropConn(conn)
		if ctx.Err() != nil {
			return
		}
		x.logger.Warn("lost the X11 clipboard connection, reconnecting", "delay", delay)
		if !sleep(ctx, delay) {
			return
		}
		delay = min(2*delay, maxReconnectDelay)
	}
}

// publish delivers a snapshot to every watcher. A watcher that cannot keep up
// loses the snapshot.
func (x *X11) publish(snapshot ports.Snapshot) {
	x.state.subMu.Lock()
	defer x.state.subMu.Unlock()
	for ch := range x.state.subs {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

// handleClear drops the served content after another client took the selection.
// A clear that follows our own takeover leaves the content in place.
func (s *server) handleClear() {
	owner, err := await(s.ctx, func() (*xproto.GetSelectionOwnerReply, error) {
		return xproto.GetSelectionOwner(s.conn.conn, s.conn.atoms.clipboard).Reply()
	})
	if err == nil && owner.Owner == s.conn.win {
		return
	}
	s.x.clearOwnership()
}

// clearOwnership drops the served content after another client took the
// selection.
func (x *X11) clearOwnership() {
	x.state.ownedMu.Lock()
	had := x.state.owned != nil
	x.state.owned = nil
	x.state.ownedMu.Unlock()
	if had {
		x.logger.Info("lost the clipboard selection")
	}
}

// sleep waits for the delay and reports false when the context ended.
func sleep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// atomSet holds the interned atoms of one connection.
type atomSet struct {
	clipboard        xproto.Atom
	targets          xproto.Atom
	timestamp        xproto.Atom
	integer          xproto.Atom
	utf8String       xproto.Atom
	textPlain        xproto.Atom
	textPlainUTF8    xproto.Atom
	textHTML         xproto.Atom
	imagePNG         xproto.Atom
	imageBMP         xproto.Atom
	imageTIFF        xproto.Atom
	imageJPEG        xproto.Atom
	uriList          xproto.Atom
	gnomeFiles       xproto.Atom
	stringAtom       xproto.Atom
	text             xproto.Atom
	incr             xproto.Atom
	multiple         xproto.Atom
	atomPair         xproto.Atom
	atomType         xproto.Atom
	marker           xproto.Atom
	passwordHint     xproto.Atom
	passwordHintMIME xproto.Atom
	selectionProp    xproto.Atom
	scratch          xproto.Atom
}

// x11Conn is one X connection with its InputOnly window and atoms.
type x11Conn struct {
	conn   *xgb.Conn
	win    xproto.Window
	atoms  atomSet
	lastTi atomic.Uint32
	nameMu sync.Mutex
	names  map[atom]string
}

// dial connects to the X server and prepares the window that owns the selection
// and receives its events.
func dial(display string) (*x11Conn, error) {
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, err
	}
	c := &x11Conn{conn: conn, names: make(map[atom]string)}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	screen := xproto.Setup(conn).DefaultScreen(conn)
	win, err := xproto.NewWindowId(conn)
	if err != nil {
		return nil, fmt.Errorf("clipboard: window id: %w", err)
	}
	err = xproto.CreateWindowChecked(conn, 0, win, screen.Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange},
	).Check()
	if err != nil {
		return nil, fmt.Errorf("clipboard: create window: %w", err)
	}
	c.win = win
	if err := xfixes.Init(conn); err != nil {
		return nil, fmt.Errorf("clipboard: XFixes extension: %w", err)
	}
	if _, err := xfixes.QueryVersion(conn, 6, 0).Reply(); err != nil {
		return nil, fmt.Errorf("clipboard: XFixes version: %w", err)
	}
	if err := c.internAtoms(); err != nil {
		return nil, err
	}
	mask := uint32(xfixes.SelectionEventMaskSetSelectionOwner |
		xfixes.SelectionEventMaskSelectionWindowDestroy |
		xfixes.SelectionEventMaskSelectionClientClose)
	if err := xfixes.SelectSelectionInputChecked(conn, win, c.atoms.clipboard, mask).Check(); err != nil {
		return nil, fmt.Errorf("clipboard: select selection events: %w", err)
	}
	// The TIMESTAMP target needs a server timestamp. A property change is the
	// cheapest way to obtain one: the notification carries the current time.
	xproto.ChangeProperty(conn, xproto.PropModeReplace, win, c.atoms.scratch, c.atoms.stringAtom, 8, 1, []byte{0})
	ok = true
	return c, nil
}

// internAtoms interns every atom the clipboard uses.
func (c *x11Conn) internAtoms() error {
	table := []struct {
		name string
		dst  *xproto.Atom
	}{
		{atomCLIPBOARD, &c.atoms.clipboard},
		{atomTARGETS, &c.atoms.targets},
		{atomTIMESTAMP, &c.atoms.timestamp},
		{atomINTEGER, &c.atoms.integer},
		{atomUTF8String, &c.atoms.utf8String},
		{atomTextPlain, &c.atoms.textPlain},
		{atomTextPlainUTF8, &c.atoms.textPlainUTF8},
		{atomTextHTML, &c.atoms.textHTML},
		{MIMEImagePNG, &c.atoms.imagePNG},
		{MIMEImageBMP, &c.atoms.imageBMP},
		{MIMEImageTIFF, &c.atoms.imageTIFF},
		{MIMEImageJPEG, &c.atoms.imageJPEG},
		{MIMEURIList, &c.atoms.uriList},
		{MIMEGNOMECopiedFiles, &c.atoms.gnomeFiles},
		{atomSTRING, &c.atoms.stringAtom},
		{atomTEXT, &c.atoms.text},
		{atomINCR, &c.atoms.incr},
		{atomMULTIPLE, &c.atoms.multiple},
		{atomATOMPair, &c.atoms.atomPair},
		{atomATOMType, &c.atoms.atomType},
		{MarkerTarget, &c.atoms.marker},
		{PasswordManagerHint, &c.atoms.passwordHint},
		{PasswordManagerHintMIME, &c.atoms.passwordHintMIME},
		{atomSelectionProp, &c.atoms.selectionProp},
		{atomScratchProp, &c.atoms.scratch},
	}
	for _, item := range table {
		value, err := c.intern(item.name)
		if err != nil {
			return err
		}
		*item.dst = value
		c.nameMu.Lock()
		c.names[atom(value)] = item.name
		c.nameMu.Unlock()
	}
	return nil
}

// intern returns the atom of a name.
func (c *x11Conn) intern(name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(c.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("clipboard: intern atom %q: %w", name, err)
	}
	if reply.Atom == 0 {
		return 0, fmt.Errorf("clipboard: intern atom %q: no atom", name)
	}
	return reply.Atom, nil
}

// name returns the name of an atom.
func (c *x11Conn) name(value atom) string {
	c.nameMu.Lock()
	name, ok := c.names[value]
	c.nameMu.Unlock()
	if ok {
		return name
	}
	reply, err := xproto.GetAtomName(c.conn, xproto.Atom(value)).Reply()
	if err != nil || reply.Name == "" {
		return ""
	}
	c.nameMu.Lock()
	c.names[value] = reply.Name
	c.nameMu.Unlock()
	return reply.Name
}

// observe remembers the server timestamp of an event.
func (c *x11Conn) observe(value uint32) {
	if value == 0 {
		return
	}
	c.lastTi.Store(value)
}

// own takes the CLIPBOARD selection and verifies that the server accepted it.
func own(conn *x11Conn, content *payload) error {
	err := xproto.SetSelectionOwnerChecked(conn.conn, conn.win, conn.atoms.clipboard, xproto.TimeCurrentTime).Check()
	if err != nil {
		return err
	}
	reply, err := xproto.GetSelectionOwner(conn.conn, conn.atoms.clipboard).Reply()
	if err != nil {
		return err
	}
	if reply.Owner != conn.win {
		return errors.New("another client owns the clipboard selection")
	}
	return nil
}

// server owns the event loop of one connection.
type server struct {
	x    *X11
	ctx  context.Context
	conn *x11Conn
	// props carries property notifications for INCR transfers.
	props chan xproto.PropertyNotifyEvent

	readMu  sync.Mutex
	pending atomic.Bool

	waitMu sync.Mutex
	waiter *selectionWaiter

	incrMu    sync.Mutex
	transfers map[incrKey]*incrTransfer
}

// selectionWaiter waits for the reply to ConvertSelection.
type selectionWaiter struct {
	target   xproto.Atom
	property xproto.Atom
	notify   chan xproto.SelectionNotifyEvent
}

// incrKey identifies one outgoing INCR transfer.
type incrKey struct {
	requestor xproto.Window
	property  atom
}

// incrTransfer is one value being served through INCR.
type incrTransfer struct {
	valueType atom
	data      []byte
	offset    int
	// lastWrite is the server time of the last property write. Delete
	// notifications older than it belong to an earlier transfer that used the
	// same property.
	lastWrite uint32
}

// serve reads events until the connection breaks or the context ends.
func (x *X11) serve(ctx context.Context, conn *x11Conn) {
	s := &server{
		x:         x,
		ctx:       ctx,
		conn:      conn,
		props:     make(chan xproto.PropertyNotifyEvent, 64),
		transfers: make(map[incrKey]*incrTransfer),
	}
	x.state.serverMu.Lock()
	x.state.server = s
	x.state.serverMu.Unlock()
	defer func() {
		x.state.serverMu.Lock()
		if x.state.server == s {
			x.state.server = nil
		}
		x.state.serverMu.Unlock()
	}()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.conn.Close()
		case <-done:
		}
	}()
	defer conn.conn.Close()
	for {
		event, err := conn.conn.WaitForEvent()
		if err != nil {
			if ctx.Err() == nil {
				x.logger.Warn("the X11 clipboard connection failed", "error", err)
			}
			return
		}
		if event == nil {
			return
		}
		switch e := event.(type) {
		case xfixes.SelectionNotifyEvent:
			conn.observe(uint32(e.Timestamp))
			if e.Selection != conn.atoms.clipboard {
				continue
			}
			if e.Subtype != xfixes.SelectionEventSetSelectionOwner || e.Owner == 0 {
				continue
			}
			go s.readSnapshot()
		case xproto.SelectionNotifyEvent:
			conn.observe(uint32(e.Time))
			s.deliverNotify(e)
		case xproto.SelectionRequestEvent:
			conn.observe(uint32(e.Time))
			s.handleRequest(e)
		case xproto.SelectionClearEvent:
			conn.observe(uint32(e.Time))
			if e.Selection == conn.atoms.clipboard {
				s.handleClear()
			}
		case xproto.PropertyNotifyEvent:
			conn.observe(uint32(e.Time))
			if e.Atom == conn.atoms.scratch {
				continue
			}
			select {
			case s.props <- e:
			default:
			}
			s.observeWrite(e)
			s.handleProperty(e)
		}
	}
}

// deliverNotify hands a conversion reply to the waiting reader.
func (s *server) deliverNotify(event xproto.SelectionNotifyEvent) {
	s.waitMu.Lock()
	waiter := s.waiter
	s.waitMu.Unlock()
	if waiter == nil || event.Requestor != s.conn.win {
		return
	}
	if event.Property != waiter.property || event.Target != waiter.target {
		return
	}
	select {
	case waiter.notify <- event:
	default:
	}
}

// readSnapshot reads the content of the new selection owner and publishes it.
// A change that arrives during a read is read again after it.
func (s *server) readSnapshot() {
	if !s.readMu.TryLock() {
		s.pending.Store(true)
		return
	}
	defer s.readMu.Unlock()
	for {
		s.readCurrent()
		if !s.pending.Swap(false) {
			return
		}
	}
}

// Read returns the state of the clipboard now. The report is empty when the
// clipboard has no owner or advertises no readable targets.
func (x *X11) Read(ctx context.Context) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, writeConnectTimeout)
	defer cancel()
	s, err := x.currentServer(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("clipboard: read: %w", err)
	}
	s.readMu.Lock()
	defer s.readMu.Unlock()
	report, _ := s.snapshot()
	return report, nil
}

// currentServer returns the event loop of the current connection, waiting for
// one to start.
func (x *X11) currentServer(ctx context.Context) (*server, error) {
	x.start()
	for {
		x.state.serverMu.Lock()
		s := x.state.server
		x.state.serverMu.Unlock()
		if s != nil {
			return s, nil
		}
		if !sleep(ctx, 20*time.Millisecond) {
			return nil, ctx.Err()
		}
	}
}

// readCurrent publishes the content of the current owner and reports it to the
// observer.
func (s *server) readCurrent() {
	report, ok := s.snapshot()
	if !ok {
		return
	}
	s.x.publish(report.Snapshot)
	if s.x.onReport != nil {
		s.x.onReport(report)
	}
}

// snapshot reads the state of the current owner. It reports false when the
// clipboard has nothing the backend reads, but the report still carries the
// advertised targets.
func (s *server) snapshot() (Report, bool) {
	conn := s.conn
	report := Report{At: time.Now()}
	owner, err := await(s.ctx, func() (*xproto.GetSelectionOwnerReply, error) {
		return xproto.GetSelectionOwner(conn.conn, conn.atoms.clipboard).Reply()
	})
	if err != nil || owner.Owner == 0 {
		return report, false
	}
	report.Owner = uint64(owner.Owner)
	report.OwnerIsUs = owner.Owner == conn.win
	owned := report.OwnerIsUs
	targets, ok := s.convert(atom(conn.atoms.targets), s.x.targetsTimeout)
	if !ok || targets.format != 32 {
		return report, false
	}
	names := make(targetList, 0, len(targets.data)/4)
	for _, value := range parseAtomList(targets.data) {
		names = append(names, conn.name(value))
	}
	report.Targets = names
	if names.has(MarkerTarget) {
		owned = true
	}
	if Sensitive(names) {
		s.x.logger.Info("skipped a sensitive clipboard value", "targets", len(names))
		report.Snapshot = ports.Snapshot{Sensitive: true, OwnedByUs: owned}
		return report, true
	}
	representations := make([]ports.Representation, 0, 3+maxFiles)
	if target, latin1 := names.pickPlain(); target != "" {
		if value, ok := s.readValueTarget(target, s.x.valueTimeout); ok && len(value) > 0 {
			representations = append(representations, ports.Representation{
				MIME: MIMETextPlain,
				Data: decodePlain(value, latin1),
			})
		}
	}
	if target := names.pickHTML(); target != "" {
		if value, ok := s.readValueTarget(target, s.x.valueTimeout); ok && len(value) > 0 {
			representations = append(representations, ports.Representation{
				MIME: MIMETextHTML,
				Data: decodeHTML(value),
			})
		}
	}
	imageBytes := 0
	if target := names.pickImage(); target != "" {
		if value, ok := s.readValueTarget(target, s.x.valueTimeout); ok && len(value) > 0 {
			png, err := readImagePNG(target, value)
			switch {
			case err != nil:
				s.x.logger.Debug("skipped a clipboard image", "target", target, "bytes", len(value), "error", err)
			case len(png) > 0:
				imageBytes = len(png)
				representations = append(representations, ports.Representation{MIME: MIMEImagePNG, Data: png})
			}
		}
	}
	fileList := []string(nil)
	if target := names.pickFiles(); target != "" {
		if value, ok := s.readValueTarget(target, s.x.valueTimeout); ok && len(value) > 0 {
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
	s.x.logger.Info("read the clipboard", "plain_bytes", plainBytes, "html_bytes", htmlBytes,
		"png_bytes", imageBytes, "files", len(fileList), "owned_by_us", owned)
	return report, true
}

// readValueTarget reads one target with its own timeout.
func (s *server) readValueTarget(target string, timeout time.Duration) ([]byte, bool) {
	value := s.conn.lookup(target)
	if value == 0 {
		return nil, false
	}
	read, ok := s.convert(value, timeout)
	if !ok {
		return nil, false
	}
	return read.data, true
}

// parseFileList returns the paths of the files listed by the target.
func parseFileList(target string, payload []byte) []string {
	switch target {
	case MIMEGNOMECopiedFiles:
		return filePaths(parseGNOMECopiedFiles(payload))
	case MIMEURIList:
		return filePaths(parseURIList(payload))
	default:
		return nil
	}
}

// lookup returns the interned atom of a protocol name.
func (c *x11Conn) lookup(name string) atom {
	c.nameMu.Lock()
	defer c.nameMu.Unlock()
	for value, known := range c.names {
		if known == name {
			return value
		}
	}
	return 0
}

// property is one property value read from the window.
type property struct {
	typ    atom
	format byte
	data   []byte
}

// convert asks the selection owner for a target and reads the property it
// writes.
func (s *server) convert(target atom, timeout time.Duration) (property, bool) {
	conn := s.conn
	waiter := &selectionWaiter{
		target:   xproto.Atom(target),
		property: conn.atoms.selectionProp,
		notify:   make(chan xproto.SelectionNotifyEvent, 1),
	}
	s.waitMu.Lock()
	s.waiter = waiter
	s.waitMu.Unlock()
	defer func() {
		s.waitMu.Lock()
		if s.waiter == waiter {
			s.waiter = nil
		}
		s.waitMu.Unlock()
	}()
	s.drainProps()
	xproto.DeleteProperty(conn.conn, conn.win, waiter.property)
	xproto.ConvertSelection(conn.conn, conn.win, conn.atoms.clipboard, xproto.Atom(target), waiter.property, xproto.TimeCurrentTime)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var notify xproto.SelectionNotifyEvent
	select {
	case notify = <-waiter.notify:
	case <-timer.C:
		return property{}, false
	case <-s.ctx.Done():
		return property{}, false
	}
	if notify.Property == 0 {
		return property{}, false
	}
	value, err := s.readProperty(waiter.property, timeout)
	if err != nil {
		return property{}, false
	}
	if value.typ != atom(conn.atoms.incr) {
		return value, true
	}
	data, err := s.readIncrement(waiter.property)
	if err != nil {
		s.x.logger.Debug("could not read an incremental clipboard value", "error", err)
		return property{}, false
	}
	return property{typ: value.typ, format: 8, data: data}, true
}

// readProperty reads a property in chunks until no bytes remain and then
// deletes it. The deletion is the signal the owner of an INCR transfer waits
// for before writing the next chunk.
func (s *server) readProperty(value xproto.Atom, timeout time.Duration) (property, error) {
	conn := s.conn
	units := uint32(propertyChunk / 4)
	var (
		data      []byte
		offset    uint32
		valueType atom
		format    byte
	)
	for {
		ctx, cancel := context.WithTimeout(s.ctx, timeout)
		reply, err := await(ctx, func() (*xproto.GetPropertyReply, error) {
			return xproto.GetProperty(conn.conn, false, conn.win, value, xproto.AtomAny, offset, units).Reply()
		})
		cancel()
		if err != nil {
			return property{}, err
		}
		if offset == 0 {
			valueType = atom(reply.Type)
			format = reply.Format
		}
		data = append(data, reply.Value...)
		if len(data) > MaxValueBytes {
			return property{}, errValueTooLarge
		}
		if reply.BytesAfter == 0 {
			// A property that appeared after the read started must stay for
			// the next read, so an absent one is not deleted.
			if valueType != 0 {
				xproto.DeleteProperty(conn.conn, conn.win, value)
			}
			return property{typ: valueType, format: format, data: data}, nil
		}
		offset += propertyAdvance(reply.ValueLen, reply.Format)
	}
}

// propertyAdvance returns how many four byte units a read moved forward.
func propertyAdvance(items uint32, format byte) uint32 {
	if format == 0 {
		return 0
	}
	return (items*uint32(format)/8 + 3) / 4
}

// readIncrement collects the chunks of an INCR transfer.
func (s *server) readIncrement(value xproto.Atom) ([]byte, error) {
	ctx, cancel := context.WithTimeout(s.ctx, maxTransferTime)
	defer cancel()
	return readValue(ctx, incrSource{server: s, property: value}, MaxValueBytes)
}

// drainProps drops property notifications left by earlier transfers.
func (s *server) drainProps() {
	for {
		select {
		case <-s.props:
		default:
			return
		}
	}
}

// incrSource reads one INCR chunk per call.
type incrSource struct {
	server   *server
	property xproto.Atom
}

// next waits for the next chunk of the transfer and reports whether more
// chunks follow.
func (s incrSource) next(ctx context.Context) ([]byte, bool, error) {
	conn := s.server.conn
	for {
		wait, cancel := context.WithTimeout(ctx, s.server.x.valueTimeout)
		select {
		case event := <-s.server.props:
			cancel()
			if event.Window != conn.win || event.Atom != s.property || event.State != xproto.PropertyNewValue {
				continue
			}
			value, err := s.server.readProperty(s.property, s.server.x.valueTimeout)
			if err != nil {
				return nil, false, err
			}
			if len(value.data) > 0 {
				return value.data, true, nil
			}
			if value.typ != 0 {
				// A zero length property of the value type ends the transfer.
				return nil, false, nil
			}
			// The notification belongs to the INCR property itself, which the
			// requestor already read and deleted; wait for the first chunk.
		case <-wait.Done():
			cancel()
			return nil, false, wait.Err()
		}
	}
}

// handleRequest answers a SelectionRequest from another client.
func (s *server) handleRequest(event xproto.SelectionRequestEvent) {
	if event.Owner != s.conn.win || event.Selection != s.conn.atoms.clipboard {
		s.notify(event, 0)
		return
	}
	s.x.state.ownedMu.Lock()
	content := s.x.state.owned
	s.x.state.ownedMu.Unlock()
	if content == nil {
		s.notify(event, 0)
		return
	}
	property := event.Property
	if property == 0 {
		property = event.Target
	}
	if atom(event.Target) == atom(s.conn.atoms.multiple) {
		s.handleMultiple(event, content)
		return
	}
	valueType, format, data, ok := s.valueFor(content, atom(event.Target))
	if !ok {
		s.notify(event, 0)
		return
	}
	s.send(event, atom(property), valueType, format, data)
}

// valueFor returns the type, format and payload of a target.
func (s *server) valueFor(content *payload, target atom) (atom, byte, []byte, bool) {
	a := s.conn.atoms
	switch target {
	case atom(a.targets):
		return atom(a.atomType), 32, encodeAtomList(s.advertisedTargets(content)), true
	case atom(a.timestamp):
		return atom(a.integer), 32, encodeUint32(s.conn.lastTi.Load()), true
	case atom(a.utf8String), atom(a.textPlain), atom(a.textPlainUTF8):
		plain := content.plainValue()
		if plain == nil {
			return 0, 0, nil, false
		}
		return target, 8, plain, true
	case atom(a.stringAtom), atom(a.text):
		plain := content.plainValue()
		if plain == nil {
			return 0, 0, nil, false
		}
		return atom(a.stringAtom), 8, encodeLatin1(plain), true
	case atom(a.textHTML):
		if content.html == nil {
			return 0, 0, nil, false
		}
		return target, 8, content.html, true
	case atom(a.imagePNG):
		if content.png == nil {
			return 0, 0, nil, false
		}
		return target, 8, content.png, true
	case atom(a.uriList):
		if len(content.files) == 0 {
			return 0, 0, nil, false
		}
		return target, 8, encodeURIList(content.files), true
	case atom(a.gnomeFiles):
		if len(content.files) == 0 {
			return 0, 0, nil, false
		}
		return target, 8, encodeGNOMECopiedFiles(content.files), true
	case atom(a.marker):
		return atom(a.marker), 8, []byte(content.itemID), true
	default:
		return 0, 0, nil, false
	}
}

// plainValue returns the plain text the clipboard serves. A file list is served
// as newline separated paths for terminals.
func (p *payload) plainValue() []byte {
	if p.plain != nil {
		return p.plain
	}
	if len(p.files) > 0 {
		return encodePlainPaths(p.files)
	}
	return nil
}

// advertisedTargets lists the targets of the served content.
func (s *server) advertisedTargets(content *payload) []atom {
	a := s.conn.atoms
	targets := []atom{
		atom(a.targets),
		atom(a.timestamp),
		atom(a.multiple),
		atom(a.marker),
	}
	if content.plainValue() != nil {
		targets = append(targets,
			atom(a.utf8String),
			atom(a.textPlainUTF8),
			atom(a.textPlain),
			atom(a.stringAtom),
			atom(a.text),
		)
	}
	if content.html != nil {
		targets = append(targets, atom(a.textHTML))
	}
	if content.png != nil {
		targets = append(targets, atom(a.imagePNG))
	}
	if len(content.files) > 0 {
		targets = append(targets, atom(a.gnomeFiles), atom(a.uriList))
	}
	return targets
}

// handleMultiple answers a MULTIPLE request: every pair carries a target and the
// property the requestor wants it in.
func (s *server) handleMultiple(event xproto.SelectionRequestEvent, content *payload) {
	ctx, cancel := context.WithTimeout(s.ctx, s.x.valueTimeout)
	reply, err := await(ctx, func() (*xproto.GetPropertyReply, error) {
		return xproto.GetProperty(s.conn.conn, false, event.Requestor, event.Property, s.conn.atoms.atomPair, 0, propertyChunk/4).Reply()
	})
	cancel()
	if err != nil || reply.Format != 32 {
		s.notify(event, 0)
		return
	}
	pairs := parseAtomList(reply.Value)
	if len(pairs)%2 != 0 {
		s.notify(event, 0)
		return
	}
	updated := make([]atom, len(pairs))
	for i := 0; i+1 < len(pairs); i += 2 {
		property := pairs[i+1]
		valueType, format, data, ok := s.valueFor(content, pairs[i])
		if !ok || property == 0 || len(data) > incrThreshold {
			// A value above the inline limit needs the INCR protocol, which
			// MULTIPLE does not use; the requestor asks for it separately.
			updated[i+1] = 0
			continue
		}
		xproto.ChangeProperty(s.conn.conn, xproto.PropModeReplace, event.Requestor, xproto.Atom(property), xproto.Atom(valueType), format, dataLen(data, format), data)
		updated[i+1] = property
	}
	xproto.ChangeProperty(s.conn.conn, xproto.PropModeReplace, event.Requestor, event.Property, s.conn.atoms.atomPair, 32, uint32(len(updated)), encodeAtomList(updated))
	s.notify(event, atom(event.Property))
}

// send writes a value into the requestor property, through INCR above the
// threshold.
func (s *server) send(event xproto.SelectionRequestEvent, property atom, valueType atom, format byte, data []byte) {
	conn := s.conn
	if len(data) > incrThreshold {
		// The owner follows an INCR transfer through property notifications of
		// the requestor window.
		xproto.ChangeWindowAttributes(conn.conn, event.Requestor, xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange})
		s.incrMu.Lock()
		s.transfers[incrKey{requestor: event.Requestor, property: property}] = &incrTransfer{valueType: valueType, data: data}
		s.incrMu.Unlock()
		size := encodeUint32(uint32(len(data)))
		xproto.ChangeProperty(conn.conn, xproto.PropModeReplace, event.Requestor, xproto.Atom(property), conn.atoms.incr, 32, 1, size)
		s.notify(event, property)
		return
	}
	xproto.ChangeProperty(conn.conn, xproto.PropModeReplace, event.Requestor, xproto.Atom(property), xproto.Atom(valueType), format, dataLen(data, format), data)
	s.notify(event, property)
}

// observeWrite records the time of a property write of an outgoing transfer.
func (s *server) observeWrite(event xproto.PropertyNotifyEvent) {
	if event.State != xproto.PropertyNewValue {
		return
	}
	key := incrKey{requestor: event.Window, property: atom(event.Atom)}
	s.incrMu.Lock()
	if transfer, ok := s.transfers[key]; ok {
		transfer.lastWrite = uint32(event.Time)
	}
	s.incrMu.Unlock()
}

// propertyExists reports whether the requestor still holds the property of a
// transfer.
func (s *server) propertyExists(key incrKey) bool {
	ctx, cancel := context.WithTimeout(s.ctx, s.x.valueTimeout)
	defer cancel()
	reply, err := await(ctx, func() (*xproto.GetPropertyReply, error) {
		return xproto.GetProperty(s.conn.conn, false, key.requestor, xproto.Atom(key.property), xproto.AtomAny, 0, 1).Reply()
	})
	return err == nil && reply.Type != 0
}

// handleProperty writes the next INCR chunk when the requestor consumed the
// previous one.
func (s *server) handleProperty(event xproto.PropertyNotifyEvent) {
	if event.State != xproto.PropertyDelete {
		return
	}
	key := incrKey{requestor: event.Window, property: atom(event.Atom)}
	s.incrMu.Lock()
	transfer, ok := s.transfers[key]
	if !ok {
		s.incrMu.Unlock()
		return
	}
	// A delete of an earlier instance of the property must not consume the
	// current chunk. The write of the current instance always happens first and
	// carries a server time.
	if !s.consumed(key, event, transfer.lastWrite) {
		s.incrMu.Unlock()
		return
	}
	if transfer.offset >= len(transfer.data) {
		delete(s.transfers, key)
		s.incrMu.Unlock()
		xproto.ChangeProperty(s.conn.conn, xproto.PropModeReplace, key.requestor, xproto.Atom(key.property), xproto.Atom(transfer.valueType), 8, 0, nil)
		return
	}
	remaining := len(transfer.data) - transfer.offset
	size := min(remaining, incrChunk)
	chunk := transfer.data[transfer.offset : transfer.offset+size]
	transfer.offset += size
	valueType := transfer.valueType
	s.incrMu.Unlock()
	xproto.ChangeProperty(s.conn.conn, xproto.PropModeReplace, key.requestor, xproto.Atom(key.property), xproto.Atom(valueType), 8, uint32(size), chunk)
}

// consumed reports whether a delete notification refers to the current
// property instance of a transfer.
func (s *server) consumed(key incrKey, event xproto.PropertyNotifyEvent, lastWrite uint32) bool {
	if lastWrite == 0 {
		return false
	}
	switch diff := int32(uint32(event.Time) - lastWrite); {
	case diff < 0:
		return false
	case diff > 0:
		return true
	default:
		// The transfer and the requestor share a millisecond. The current
		// instance is still there when the notification belongs to an earlier
		// one.
		return !s.propertyExists(key)
	}
}

// notify sends the reply of a selection request. A zero property reports that
// the request failed.
func (s *server) notify(event xproto.SelectionRequestEvent, property atom) {
	reply := xproto.SelectionNotifyEvent{
		Time:      event.Time,
		Requestor: event.Requestor,
		Selection: event.Selection,
		Target:    event.Target,
		Property:  xproto.Atom(property),
	}
	xproto.SendEvent(s.conn.conn, false, event.Requestor, 0, string(reply.Bytes()))
}

// encodeAtomList encodes atoms for a 32 bit property.
func encodeAtomList(values []atom) []byte {
	data := make([]byte, 0, len(values)*4)
	for _, value := range values {
		data = binary.LittleEndian.AppendUint32(data, uint32(value))
	}
	return data
}

// encodeUint32 encodes a single 32 bit property value.
func encodeUint32(value uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, value)
}

// dataLen returns the number of items of a property value.
func dataLen(data []byte, format byte) uint32 {
	if format == 32 {
		return uint32(len(data) / 4)
	}
	return uint32(len(data))
}

// await runs an X request and gives up when the context ends. The request
// finishes in the background; a connection close releases it.
func await[T any](ctx context.Context, call func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := call()
		done <- result{value: value, err: err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
