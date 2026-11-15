// Package clipboard implements the Win32 clipboard as a ports.Clipboard.
//
// The watcher listens for clipboard changes on a message-only window instead of
// polling the change counter. Content tagged with one of the exclusion formats
// is reported as sensitive and its payload is never read. Content written by
// Write carries MarkerFormat, which lets the watcher and other clipboard
// consumers tell own writes apart.
//
// The clipboard is reachable only on Windows. Elsewhere the operations return
// ErrUnsupported.
package clipboard

import (
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
)

var (
	// ErrUnsupported reports that the Win32 clipboard is unavailable on this
	// platform.
	ErrUnsupported = errors.New("clipboard: the Win32 clipboard is available on Windows only")
	// ErrUnsupportedContent reports content the clipboard cannot write.
	ErrUnsupportedContent = errors.New("clipboard: unsupported content")
)

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

var _ ports.Clipboard = (*Clipboard)(nil)

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

// textOf splits content into the text payloads the clipboard writes.
func textOf(content ports.Content) (textPayload, error) {
	var payload textPayload
	for _, representation := range content.Representations {
		switch representation.MIME {
		case MIMETextPlain:
			payload.plain, payload.hasPlain = representation.Data, true
		case MIMETextHTML:
			payload.html, payload.hasHTML = representation.Data, true
		default:
			return textPayload{}, fmt.Errorf("%w: mime %q", ErrUnsupportedContent, representation.MIME)
		}
	}
	if !payload.hasPlain && !payload.hasHTML {
		return textPayload{}, ErrUnsupportedContent
	}
	return payload, nil
}
