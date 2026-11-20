// Package clipboard implements the X11 CLIPBOARD selection as a ports.Clipboard.
//
// The watcher subscribes to ownership changes of the CLIPBOARD selection through
// the XFixes extension and reads text and HTML from the new owner. Only
// CLIPBOARD is watched: PRIMARY is not synchronized. Content tagged with the
// password manager hint is reported as sensitive and its payload is never read.
//
// Write takes ownership of CLIPBOARD and answers selection requests while the
// content is held in memory. Written content carries the marker target with the
// clip item id, which lets the watcher and other clipboard consumers tell own
// writes apart. The selection is served through XWayland as well, so the
// package works in a GNOME Wayland session.
//
// The clipboard is reachable only on Linux. Elsewhere the operations return
// ErrUnsupported.
package clipboard

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// MIME types the clipboard reads and writes.
const (
	MIMETextPlain = "text/plain"
	MIMETextHTML  = "text/html"
)

// MarkerTarget is the X11 target that marks content written by Unison. Its
// payload is the clip item id.
const MarkerTarget = "application/x-unison-origin"

// DefaultTargetsTimeout bounds the answer of the selection owner to a TARGETS
// request.
const DefaultTargetsTimeout = time.Second

// DefaultValueTimeout bounds one part of a clipboard value: one property read or
// one INCR chunk. A stuck owner is skipped instead of blocking the watcher.
const DefaultValueTimeout = time.Second

// MaxValueBytes limits one clipboard value, the same way the other platforms do.
const MaxValueBytes = 64 << 20

var (
	// ErrNoDisplay reports that DISPLAY is not set, so there is no X server to
	// talk to.
	ErrNoDisplay = errors.New("clipboard: DISPLAY is not set")
	// ErrUnsupported reports that the X11 clipboard is unavailable on this
	// platform.
	ErrUnsupported = errors.New("clipboard: the X11 clipboard is available on Linux only")
	// ErrUnsupportedContent reports content the clipboard cannot write.
	ErrUnsupportedContent = errors.New("clipboard: unsupported content")
)

// X11Options configures an X11 clipboard.
type X11Options struct {
	// Display is the X display in the X11 DISPLAY format. The DISPLAY
	// environment variable is used when empty.
	Display string
	// TargetsTimeout bounds the answer to a TARGETS request. Defaults to
	// DefaultTargetsTimeout.
	TargetsTimeout time.Duration
	// ValueTimeout bounds one part of a value. Defaults to DefaultValueTimeout.
	ValueTimeout time.Duration
	// Logger receives connection and transfer events. No content is logged.
	Logger *slog.Logger
}

// payload is the content the clipboard serves while it owns the selection.
type payload struct {
	plain  []byte
	html   []byte
	itemID string
}

// payloadOf splits content into the representations the clipboard serves. Images
// and files are not part of this backend.
func payloadOf(content ports.Content) (payload, error) {
	var result payload
	for _, representation := range content.Representations {
		switch {
		case representation.FilePath != "":
			return payload{}, fmt.Errorf("%w: file %q", ErrUnsupportedContent, representation.MIME)
		case representation.MIME == MIMETextPlain:
			result.plain = representation.Data
		case representation.MIME == MIMETextHTML:
			result.html = representation.Data
		default:
			return payload{}, fmt.Errorf("%w: mime %q", ErrUnsupportedContent, representation.MIME)
		}
	}
	result.itemID = content.ItemID
	if result.plain == nil && result.html == nil {
		return payload{}, ErrUnsupportedContent
	}
	return result, nil
}
