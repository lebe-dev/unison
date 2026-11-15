// Package clipboard implements the macOS pasteboard as a ports.Clipboard.
//
// The pasteboard has no change notification, so the watcher polls the change
// counter. Content tagged with one of the confidentiality or transience marker
// types is reported as sensitive and its payload is never read. Content written
// by Write carries MarkerType, which lets the watcher and other clipboard
// consumers tell own writes apart.
//
// The pasteboard is reachable only in a cgo build on macOS. Everywhere else the
// operations return ErrUnsupported.
package clipboard

import (
	"errors"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	// DefaultPollInterval is how often the watcher checks the change counter.
	DefaultPollInterval = 300 * time.Millisecond
	// MarkerType marks pasteboard content written by Unison.
	MarkerType = "app.unison.origin"
)

// MIME types the clipboard reads and writes.
const (
	MIMETextPlain = "text/plain"
	MIMETextHTML  = "text/html"
)

var (
	// ErrUnsupported reports that the pasteboard is unavailable because the
	// binary was built without cgo.
	ErrUnsupported = errors.New("clipboard: pasteboard requires cgo")
	// ErrUnsupportedContent reports content the clipboard cannot write.
	ErrUnsupportedContent = errors.New("clipboard: unsupported content")
)

// Behavior is the system policy for programmatic pasteboard reads.
type Behavior int

// Access behaviors, matching the values of NSPasteboardAccessBehavior.
const (
	// BehaviorDefault means the application has not triggered an access alert
	// yet. The general pasteboard treats it like BehaviorAsk.
	BehaviorDefault Behavior = iota
	// BehaviorAsk means a read may show an access alert.
	BehaviorAsk
	// BehaviorAlwaysAllow means reads never show an alert.
	BehaviorAlwaysAllow
	// BehaviorAlwaysDeny means reads are denied without an alert.
	BehaviorAlwaysDeny
)

// Options configures a Clipboard.
type Options struct {
	// PollInterval is how often the change counter is checked.
	// DefaultPollInterval is used when zero.
	PollInterval time.Duration
}

// Clipboard is a ports.Clipboard backed by the general pasteboard.
type Clipboard struct {
	interval time.Duration
}

var _ ports.Clipboard = (*Clipboard)(nil)

// New returns a Clipboard that polls the change counter at the configured
// interval.
func New(opts Options) (*Clipboard, error) {
	interval := opts.PollInterval
	if interval < 0 {
		return nil, errors.New("clipboard: negative poll interval")
	}
	if interval == 0 {
		interval = DefaultPollInterval
	}
	return &Clipboard{interval: interval}, nil
}

// PollInterval returns the configured change counter poll interval.
func (c *Clipboard) PollInterval() time.Duration { return c.interval }
