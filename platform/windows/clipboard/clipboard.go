// Package clipboard implements the Win32 clipboard as a ports.Clipboard.
//
// The watcher listens for clipboard changes on a message-only window instead of
// polling the change counter. It reads text, HTML, PNG and device independent
// bitmaps, and dropped files. Content tagged with one of the exclusion formats
// is reported as sensitive and its payload is never read.
//
// Write publishes text, an image as PNG plus a bitmap for applications without
// PNG support, and files as a dropped file list. Content written by Write
// carries MarkerFormat, which lets the watcher and other clipboard consumers
// tell own writes apart.
//
// The clipboard is reachable only on Windows. Elsewhere the operations return
// ErrUnsupported.
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// DefaultOpenTimeout is how long a clipboard operation retries while another
// application holds the clipboard open.
const DefaultOpenTimeout = time.Second

// MarkerFormat is the clipboard format that marks content written by Unison.
const MarkerFormat = "Unison.Origin"

// MIME types the clipboard reads and writes.
const (
	MIMETextPlain = "text/plain"
	MIMETextHTML  = "text/html"
	// MIMEImagePNG is the image type of a snapshot and the only image type the
	// clipboard writes.
	MIMEImagePNG = "image/png"
	// MIMEUnisonFile marks a representation that carries a file path.
	MIMEUnisonFile = "application/x-unison-file"
)

var (
	// ErrUnsupported reports that the Win32 clipboard is unavailable on this
	// platform.
	ErrUnsupported = errors.New("clipboard: the Win32 clipboard is available on Windows only")
	// ErrUnsupportedContent reports content the clipboard cannot write.
	ErrUnsupportedContent = errors.New("clipboard: unsupported content")
)

// HistoryExcluder is a clipboard that can keep written content out of the
// system clipboard history and cloud sync.
type HistoryExcluder interface {
	// WriteExcluded replaces the clipboard content and marks it as private to
	// this machine.
	WriteExcluded(ctx context.Context, content ports.Content) error
}

// Options configures a Clipboard.
type Options struct {
	// OpenTimeout bounds the retries while another application holds the
	// clipboard open. DefaultOpenTimeout is used when zero.
	OpenTimeout time.Duration
}

// Clipboard is a ports.Clipboard backed by the Win32 clipboard.
type Clipboard struct {
	openTimeout time.Duration
}

var (
	_ ports.Clipboard = (*Clipboard)(nil)
	_ HistoryExcluder = (*Clipboard)(nil)
)

// New returns a Clipboard that retries a busy clipboard for up to the
// configured timeout.
func New(opts Options) (*Clipboard, error) {
	timeout := opts.OpenTimeout
	if timeout < 0 {
		return nil, errors.New("clipboard: negative open timeout")
	}
	if timeout == 0 {
		timeout = DefaultOpenTimeout
	}
	return &Clipboard{openTimeout: timeout}, nil
}

// textPayload is the text content the clipboard writes.
type textPayload struct {
	plain    []byte
	html     []byte
	hasPlain bool
	hasHTML  bool
}

// payload is the content the clipboard writes.
type payload struct {
	text  textPayload
	png   []byte
	files []string
}

// payloadOf splits content into the parts the clipboard writes. A
// representation with a file path is a file, whatever its type.
func payloadOf(content ports.Content) (payload, error) {
	var result payload
	for _, representation := range content.Representations {
		switch {
		case representation.FilePath != "":
			result.files = append(result.files, representation.FilePath)
		case representation.MIME == MIMETextPlain:
			result.text.plain, result.text.hasPlain = representation.Data, true
		case representation.MIME == MIMETextHTML:
			result.text.html, result.text.hasHTML = representation.Data, true
		case representation.MIME == MIMEImagePNG:
			result.png = representation.Data
		default:
			return payload{}, fmt.Errorf("%w: mime %q", ErrUnsupportedContent, representation.MIME)
		}
	}
	if !result.text.hasPlain && !result.text.hasHTML && result.png == nil && len(result.files) == 0 {
		return payload{}, ErrUnsupportedContent
	}
	return result, nil
}
