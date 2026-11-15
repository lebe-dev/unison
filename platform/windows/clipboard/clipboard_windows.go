//go:build windows

package clipboard

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/lebe-dev/unison/core/ports"
)

// Clipboard formats and the names they are registered with.
const (
	// cfUnicodeText is CF_UNICODETEXT.
	cfUnicodeText = 13
	// formatHTML is the registered "HTML Format" carrying CF_HTML.
	formatHTML = "HTML Format"
	// formatExclude excludes the content from the clipboard history and cloud.
	formatExclude = "ExcludeClipboardContentFromMonitorProcessing"
	// formatHistory carries 0 when the content must not enter the history.
	formatHistory = "CanIncludeInClipboardHistory"
	// formatCloud carries 0 when the content must not leave the machine.
	formatCloud = "CanUploadToCloudClipboard"
)

// Window messages of the watcher window.
const (
	wmQuit            = 0x0012
	wmClipboardUpdate = 0x031D
)

// gmemMoveable allocates clipboard memory that the system can move.
const gmemMoveable = 0x0002

// hwndMessage makes the watcher window message-only.
const hwndMessage windows.Handle = ^windows.Handle(2)

// watchBuffer is the number of snapshots buffered before the watcher blocks.
const watchBuffer = 16

// openRetryInterval is the delay between attempts to open a busy clipboard.
const openRetryInterval = 25 * time.Millisecond

// maxClipboardFormats bounds the format enumeration of one read.
const maxClipboardFormats = 4096

// maxPayload bounds the memory a single clipboard read allocates. Larger
// content is skipped instead of risking a failed allocation.
const maxPayload = 64 << 20

// originPayload is the content of the own write marker.
const originPayload = "unison"

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procAddClipboardFormatListener    = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procCloseClipboard                = user32.NewProc("CloseClipboard")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procEmptyClipboard                = user32.NewProc("EmptyClipboard")
	procEnumClipboardFormats          = user32.NewProc("EnumClipboardFormats")
	procGetClipboardData              = user32.NewProc("GetClipboardData")
	procGetClipboardSequenceNumber    = user32.NewProc("GetClipboardSequenceNumber")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procOpenClipboard                 = user32.NewProc("OpenClipboard")
	procPostThreadMessageW            = user32.NewProc("PostThreadMessageW")
	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procRegisterClipboardFormatW      = user32.NewProc("RegisterClipboardFormatW")
	procSetClipboardData              = user32.NewProc("SetClipboardData")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procUnregisterClassW              = user32.NewProc("UnregisterClassW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalSize       = kernel32.NewProc("GlobalSize")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")

	// windowProc is the window procedure of the watcher window.
	windowProc = syscall.NewCallback(handleWindowMessage)
)

// windowSequence numbers the window classes, because every class name can be
// registered once per process.
var windowSequence atomic.Uint32

// formatIDs are the clipboard formats the adapter registers.
type formatIDs struct {
	html    uint32
	origin  uint32
	exclude uint32
	history uint32
	cloud   uint32
}

// registeredFormats returns the identifiers of the adapter formats.
var registeredFormats = sync.OnceValue(func() formatIDs {
	return formatIDs{
		html:    registerFormat(formatHTML),
		origin:  registerFormat(MarkerFormat),
		exclude: registerFormat(formatExclude),
		history: registerFormat(formatHistory),
		cloud:   registerFormat(formatCloud),
	}
})

// Watch delivers a snapshot for every clipboard change until ctx is done. The
// content present at the start of watching is not reported.
func (c *Clipboard) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listener, err := newListener()
	if err != nil {
		return nil, fmt.Errorf("clipboard: watch: %w", err)
	}
	snapshots := make(chan ports.Snapshot, watchBuffer)
	go c.watch(ctx, listener, snapshots)
	return snapshots, nil
}

// watch turns clipboard notifications into snapshots of the changed content.
func (c *Clipboard) watch(ctx context.Context, listener *listener, out chan<- ports.Snapshot) {
	defer close(out)
	defer listener.stop()
	previous := clipboardSequence()
	for {
		select {
		case <-ctx.Done():
			return
		case <-listener.wake:
		}
		sequence := clipboardSequence()
		if sequence == previous {
			continue
		}
		previous = sequence
		snapshot, ok := c.snapshot()
		if !ok {
			continue
		}
		select {
		case out <- snapshot:
		case <-ctx.Done():
			return
		}
	}
}

// Write replaces the clipboard content with the given text representations and
// marks it as written by this application.
func (c *Clipboard) Write(ctx context.Context, content ports.Content) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	text, err := textOf(content)
	if err != nil {
		return err
	}
	var htmlPayload []byte
	if text.hasHTML {
		payload, err := buildCFHTML(text.html)
		if err != nil {
			return err
		}
		htmlPayload = payload
	}

	// The clipboard belongs to the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(c.openTimeout); err != nil {
		return fmt.Errorf("clipboard: write: %w", err)
	}
	defer closeClipboard()
	if err := emptyClipboard(); err != nil {
		return fmt.Errorf("clipboard: write: %w", err)
	}
	ids := registeredFormats()
	if text.hasPlain {
		if err := putClipboardData(cfUnicodeText, encodeText(text.plain)); err != nil {
			return fmt.Errorf("clipboard: write text: %w", err)
		}
	}
	if text.hasHTML {
		if err := putClipboardData(ids.html, htmlPayload); err != nil {
			return fmt.Errorf("clipboard: write html: %w", err)
		}
	}
	if err := putClipboardData(ids.origin, []byte(originPayload)); err != nil {
		return fmt.Errorf("clipboard: write marker: %w", err)
	}
	return nil
}

// snapshot reads the current clipboard content. It reports false when the
// content carries nothing the adapter reads.
func (c *Clipboard) snapshot() (ports.Snapshot, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(c.openTimeout); err != nil {
		return ports.Snapshot{}, false
	}
	defer closeClipboard()
	return readSnapshot()
}

// readSnapshot reads the open clipboard.
func readSnapshot() (ports.Snapshot, bool) {
	present := clipboardFormatSet()
	ids := registeredFormats()
	ownedByUs := hasFormat(present, ids.origin)
	if isSensitive(present, ids) {
		return ports.Snapshot{Sensitive: true, OwnedByUs: ownedByUs}, true
	}
	representations := make([]ports.Representation, 0, 2)
	if payload, ok := clipboardData(cfUnicodeText); ok {
		if text := decodeText(payload); len(text) > 0 {
			representations = append(representations, ports.Representation{MIME: MIMETextPlain, Data: text})
		}
	}
	if payload, ok := clipboardData(ids.html); ok {
		if html := fragmentOf(payload); len(html) > 0 {
			representations = append(representations, ports.Representation{MIME: MIMETextHTML, Data: html})
		}
	}
	if len(representations) == 0 {
		return ports.Snapshot{}, false
	}
	return ports.Snapshot{
		Content:   ports.Content{Representations: representations},
		OwnedByUs: ownedByUs,
	}, true
}

// isSensitive reports content the system excluded from the clipboard history or
// cloud sync. A marker that cannot be read counts as excluded.
func isSensitive(present map[uint32]struct{}, ids formatIDs) bool {
	if hasFormat(present, ids.exclude) {
		return true
	}
	for _, id := range []uint32{ids.history, ids.cloud} {
		if !hasFormat(present, id) {
			continue
		}
		value, ok := formatDWORD(id)
		if !ok || value == 0 {
			return true
		}
	}
	return false
}

// hasFormat reports whether the clipboard carries a format. A zero identifier
// stands for a format that could not be registered and is never on the
// clipboard.
func hasFormat(present map[uint32]struct{}, format uint32) bool {
	if format == 0 {
		return false
	}
	_, ok := present[format]
	return ok
}

// clipboardFormatSet returns the formats on the open clipboard.
func clipboardFormatSet() map[uint32]struct{} {
	formats := make(map[uint32]struct{}, 8)
	current := uint32(0)
	for range maxClipboardFormats {
		next, _, _ := procEnumClipboardFormats.Call(uintptr(current))
		if next == 0 {
			break
		}
		current = uint32(next)
		formats[current] = struct{}{}
	}
	return formats
}

// clipboardData returns the content of a format from the open clipboard.
func clipboardData(format uint32) ([]byte, bool) {
	handle, _, _ := procGetClipboardData.Call(uintptr(format))
	if handle == 0 {
		return nil, false
	}
	address, _, _ := procGlobalLock.Call(handle)
	if address == 0 {
		return nil, false
	}
	defer func() {
		_, _, _ = procGlobalUnlock.Call(handle)
	}()
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 || size > maxPayload {
		return nil, false
	}
	payload := make([]byte, size)
	copy(payload, byteSlice(address, int(size)))
	return payload, true
}

// formatDWORD reads the value a format carries.
func formatDWORD(format uint32) (uint32, bool) {
	payload, ok := clipboardData(format)
	if !ok || len(payload) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(payload), true
}

// byteSlice views n bytes at the memory address a Win32 call returned.
func byteSlice(address uintptr, n int) []byte {
	return unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(nil), address)), n)
}

// openClipboard opens the clipboard for the calling thread, retrying while
// another application holds it.
func openClipboard(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		opened, _, err := procOpenClipboard.Call(0)
		if opened != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return callError("open clipboard", err)
		}
		time.Sleep(openRetryInterval)
	}
}

// closeClipboard releases the clipboard of the calling thread.
func closeClipboard() {
	_, _, _ = procCloseClipboard.Call()
}

// clipboardSequence returns the clipboard change counter.
func clipboardSequence() uint32 {
	sequence, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(sequence)
}

// emptyClipboard removes the current content from the open clipboard.
func emptyClipboard() error {
	emptied, _, err := procEmptyClipboard.Call()
	if emptied == 0 {
		return callError("empty clipboard", err)
	}
	return nil
}

// putClipboardData moves data into global memory owned by the clipboard. The
// system frees the memory when the content is replaced.
func putClipboardData(format uint32, data []byte) error {
	size := len(data)
	if size == 0 {
		size = 1
	}
	handle, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if handle == 0 {
		return callError("allocate clipboard memory", err)
	}
	address, _, err := procGlobalLock.Call(handle)
	if address == 0 {
		_, _, _ = procGlobalFree.Call(handle)
		return callError("lock clipboard memory", err)
	}
	copy(byteSlice(address, size), data)
	_, _, _ = procGlobalUnlock.Call(handle)
	stored, _, err := procSetClipboardData.Call(uintptr(format), handle)
	if stored == 0 {
		_, _, _ = procGlobalFree.Call(handle)
		return callError("set clipboard data", err)
	}
	return nil
}

// registerFormat registers a clipboard format name and returns its identifier.
func registerFormat(name string) uint32 {
	pointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	format, _, _ := syscall.SyscallN(procRegisterClipboardFormatW.Addr(), uintptr(unsafe.Pointer(pointer)))
	return uint32(format)
}

// callError describes a failed Win32 call. Calls that fail without an error
// code report success.
func callError(op string, err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 0 {
		return fmt.Errorf("%s failed", op)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// listener owns the message-only window that reports clipboard changes.
type listener struct {
	wake   chan struct{}
	ready  chan error
	thread uint32
}

// newListener creates the window and starts its message loop.
func newListener() (*listener, error) {
	listener := &listener{
		wake:  make(chan struct{}, 1),
		ready: make(chan error, 1),
	}
	go listener.run()
	if err := <-listener.ready; err != nil {
		return nil, err
	}
	return listener, nil
}

// stop ends the message loop. The quit message addresses the thread, because
// the loop may already have destroyed the window.
func (l *listener) stop() {
	_, _, _ = procPostThreadMessageW.Call(uintptr(l.thread), wmQuit, 0, 0)
}

// run owns the watcher window and its message loop. Windows delivers clipboard
// notifications only on the thread that created the window.
func (l *listener) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	window, err := createMessageWindow(l.wake)
	if err != nil {
		l.ready <- err
		return
	}
	l.thread = windows.GetCurrentThreadId()
	l.ready <- nil
	window.runLoop()
	window.close()
}

// messageWindow is the hidden window that receives clipboard notifications.
type messageWindow struct {
	handle    windows.Handle
	className *uint16
	instance  uintptr
}

// createMessageWindow creates the window and registers it as a clipboard format
// listener.
func createMessageWindow(wake chan struct{}) (*messageWindow, error) {
	name, err := windows.UTF16PtrFromString(fmt.Sprintf("UnisonClipboardWindow%d", windowSequence.Add(1)))
	if err != nil {
		return nil, fmt.Errorf("clipboard: window class name: %w", err)
	}
	instance, _, _ := procGetModuleHandleW.Call(0)
	class := wndClassEx{
		size:      uint32(unsafe.Sizeof(wndClassEx{})),
		wndProc:   windowProc,
		instance:  windows.Handle(instance),
		className: name,
	}
	atom, _, err := syscall.SyscallN(procRegisterClassExW.Addr(), uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return nil, callError("register window class", err)
	}
	window := &messageWindow{className: name, instance: instance}
	handle, _, err := syscall.SyscallN(procCreateWindowExW.Addr(),
		0, uintptr(unsafe.Pointer(name)), 0, 0,
		0, 0, 0, 0,
		uintptr(hwndMessage), 0, instance, 0,
	)
	if handle == 0 {
		window.unregisterClass()
		return nil, callError("create window", err)
	}
	window.handle = windows.Handle(handle)
	registerWindowWake(window.handle, wake)
	if listened, _, err := procAddClipboardFormatListener.Call(handle); listened == 0 {
		window.close()
		return nil, callError("add clipboard format listener", err)
	}
	return window, nil
}

// runLoop dispatches window messages until the quit message arrives.
func (w *messageWindow) runLoop() {
	for {
		var message msg
		result, _, _ := syscall.SyscallN(procGetMessageW.Addr(), uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			return
		}
		_, _, _ = syscall.SyscallN(procTranslateMessage.Addr(), uintptr(unsafe.Pointer(&message)))
		_, _, _ = syscall.SyscallN(procDispatchMessageW.Addr(), uintptr(unsafe.Pointer(&message)))
	}
}

// close removes the window and its class. It runs on the thread that created
// the window.
func (w *messageWindow) close() {
	if w.handle != 0 {
		_, _, _ = procRemoveClipboardFormatListener.Call(uintptr(w.handle))
		unregisterWindowWake(w.handle)
		_, _, _ = procDestroyWindow.Call(uintptr(w.handle))
		w.handle = 0
	}
	w.unregisterClass()
}

// unregisterClass frees the window class.
func (w *messageWindow) unregisterClass() {
	_, _, _ = syscall.SyscallN(procUnregisterClassW.Addr(), uintptr(unsafe.Pointer(w.className)), w.instance)
}

var (
	wakeMu       sync.Mutex
	wakeByWindow = map[windows.Handle]chan struct{}{}
)

// registerWindowWake ties the clipboard notifications of a window to a channel.
func registerWindowWake(window windows.Handle, wake chan struct{}) {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	wakeByWindow[window] = wake
}

// unregisterWindowWake forgets a destroyed window.
func unregisterWindowWake(window windows.Handle) {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	delete(wakeByWindow, window)
}

// notify wakes the watcher of a window without blocking the message loop.
func notify(window windows.Handle) {
	wakeMu.Lock()
	wake, ok := wakeByWindow[window]
	wakeMu.Unlock()
	if !ok {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// handleWindowMessage forwards clipboard notifications and leaves the remaining
// messages to the default procedure.
func handleWindowMessage(window windows.Handle, message uint32, wparam, lparam uintptr) uintptr {
	if message == wmClipboardUpdate {
		notify(window)
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(uintptr(window), uintptr(message), wparam, lparam)
	return result
}

// wndClassEx mirrors WNDCLASSEXW.
type wndClassEx struct {
	size        uint32
	style       uint32
	wndProc     uintptr
	classExtra  int32
	windowExtra int32
	instance    windows.Handle
	icon        windows.Handle
	cursor      windows.Handle
	background  windows.Handle
	menuName    *uint16
	className   *uint16
	smallIcon   windows.Handle
}

// msg mirrors MSG.
type msg struct {
	window windows.Handle
	id     uint32
	wparam uintptr
	lparam uintptr
	time   uint32
	point  struct{ x, y int32 }
}
