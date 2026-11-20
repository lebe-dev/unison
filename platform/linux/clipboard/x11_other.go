//go:build !linux

package clipboard

import (
	"context"
	"fmt"

	"github.com/lebe-dev/unison/core/ports"
)

// X11 is a ports.Clipboard that is unavailable outside Linux.
type X11 struct{}

var _ ports.Clipboard = (*X11)(nil)

// NewX11 returns ErrUnsupported.
func NewX11(X11Options) (*X11, error) {
	return nil, ErrUnsupported
}

// Watch returns ErrUnsupported.
func (x *X11) Watch(context.Context) (<-chan ports.Snapshot, error) {
	return nil, fmt.Errorf("clipboard: watch: %w", ErrUnsupported)
}

// Write returns ErrUnsupported.
func (x *X11) Write(context.Context, ports.Content) error {
	return fmt.Errorf("clipboard: write: %w", ErrUnsupported)
}

// Close releases nothing outside Linux.
func (x *X11) Close() error { return nil }
