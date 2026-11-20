//go:build !linux

package clipboard

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// WaylandOptions configures a clipboard of the data-control protocol.
type WaylandOptions struct {
	// Display is the Wayland socket name in the WAYLAND_DISPLAY format.
	Display string
	// RuntimeDir is the directory of the socket.
	RuntimeDir string
	// ValueTimeout bounds one part of a value read.
	ValueTimeout time.Duration
	// Logger receives connection and transfer events.
	Logger *slog.Logger
	// OnReport receives every observed clipboard state.
	OnReport func(Report)
}

// Wayland is a ports.Clipboard that is unavailable outside Linux.
type Wayland struct{}

var _ ports.Clipboard = (*Wayland)(nil)

// NewWayland returns ErrUnsupported.
func NewWayland(WaylandOptions) (*Wayland, error) {
	return nil, ErrUnsupported
}

// Watch returns ErrUnsupported.
func (w *Wayland) Watch(context.Context) (<-chan ports.Snapshot, error) {
	return nil, fmt.Errorf("clipboard: watch: %w", ErrUnsupported)
}

// Write returns ErrUnsupported.
func (w *Wayland) Write(context.Context, ports.Content) error {
	return fmt.Errorf("clipboard: write: %w", ErrUnsupported)
}

// Read returns ErrUnsupported.
func (w *Wayland) Read(context.Context) (Report, error) {
	return Report{}, fmt.Errorf("clipboard: read: %w", ErrUnsupported)
}

// Close releases nothing outside Linux.
func (w *Wayland) Close() error { return nil }
