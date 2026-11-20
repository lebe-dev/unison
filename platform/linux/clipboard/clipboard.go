// Package clipboard implements the system CLIPBOARD selection as a
// ports.Clipboard.
//
// Two backends reach the same selection. The X11 backend talks to the X server,
// which is what a GNOME session offers, also through XWayland. The Wayland
// backend uses the data-control protocol of the compositor, which KDE Plasma,
// wlroots compositors and COSMIC offer and GNOME does not.
//
// The watcher reads text, HTML, images and copied files from the new owner.
// Only CLIPBOARD is watched: PRIMARY is not synchronized. An image is reported
// as PNG; BMP, TIFF and JPEG images are converted. A copied file travels as its
// path, so a large file is never read here. Content tagged with the password
// manager hint is reported as sensitive and its payload is never read.
//
// Write takes ownership of CLIPBOARD and answers selection requests while the
// content is held in memory. Copied files are served as a Nautilus file list, an
// RFC 2483 URI list and a plain text list of paths, which is what file managers
// and terminals paste. Written content carries the marker target with the clip
// item id, which lets the watcher and other clipboard consumers tell own writes
// apart.
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
	// ErrUnsupported reports that the system clipboard is unavailable on this
	// platform.
	ErrUnsupported = errors.New("clipboard: the system clipboard is available on Linux only")
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
	// OnReport receives every observed clipboard state with the targets the
	// owner advertised. It runs on the watcher goroutine and must not block.
	OnReport func(Report)
}

// Report is one observed clipboard state for diagnostics.
type Report struct {
	// Snapshot is the content of the state.
	Snapshot ports.Snapshot
	// Targets are the target names the selection owner advertised.
	Targets []string
	// Owner identifies the owner of the selection: the X window, or the
	// protocol object of the Wayland offer. It is zero when there is none.
	Owner uint64
	// OwnerIsUs reports whether the selection belongs to this application.
	OwnerIsUs bool
	// At is when the state was read.
	At time.Time
}

// payload is the content the clipboard serves while it owns the selection.
// Copied files win over text and an image, because a file list carries the
// selection the user made.
type payload struct {
	plain  []byte
	html   []byte
	png    []byte
	files  []string
	itemID string
}

// payloadOf splits content into the parts the clipboard serves. A
// representation with a file path is a file, whatever its type.
func payloadOf(content ports.Content) (payload, error) {
	var result payload
	for _, representation := range content.Representations {
		switch {
		case representation.FilePath != "":
			result.files = append(result.files, representation.FilePath)
		case representation.MIME == MIMETextPlain:
			result.plain = representation.Data
		case representation.MIME == MIMETextHTML:
			result.html = representation.Data
		case representation.MIME == MIMEImagePNG:
			result.png = representation.Data
		default:
			return payload{}, fmt.Errorf("%w: mime %q", ErrUnsupportedContent, representation.MIME)
		}
	}
	result.itemID = content.ItemID

	if len(result.files) > 0 {
		files, err := checkedPaths(result.files)
		if err != nil {
			return payload{}, fmt.Errorf("%w: %w", ErrUnsupportedContent, err)
		}
		result.files = files
		result.plain, result.html, result.png = nil, nil, nil
		return result, nil
	}
	if result.png != nil {
		if err := checkPNG(result.png); err != nil {
			return payload{}, fmt.Errorf("%w: %w", ErrUnsupportedContent, err)
		}
	}
	if result.plain == nil && result.html == nil && result.png == nil {
		return payload{}, ErrUnsupportedContent
	}
	return result, nil
}
