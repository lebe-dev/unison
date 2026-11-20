package daemon

// Clipboard modes the daemon accepts. Not every platform offers all of them.
const (
	// ClipboardSystem is the clipboard of the platform.
	ClipboardSystem = "system"
	// ClipboardAuto picks the backend of the session.
	ClipboardAuto = "auto"
	// ClipboardX11 reads the clipboard of the X server, which reaches a Wayland
	// session through XWayland.
	ClipboardX11 = "x11"
	// ClipboardWayland reads the clipboard through the data-control protocol.
	ClipboardWayland = "wayland"
	// ClipboardNone runs the daemon without the system clipboard.
	ClipboardNone = "none"
)

// DefaultClipboardMode is the mode of a daemon started without the flag.
const DefaultClipboardMode = ClipboardSystem

// clipboardStatusReporter reports the clipboard backend for the status.
type clipboardStatusReporter interface {
	// ClipboardStatus returns the backend and the reason of the manual mode,
	// which is empty while the clipboard works.
	ClipboardStatus() (backend, note string)
}
