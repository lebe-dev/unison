//go:build !windows

package clipboard

import (
	"context"
	"fmt"

	"github.com/lebe-dev/unison/core/ports"
)

// Watch returns ErrUnsupported.
func (c *Clipboard) Watch(_ context.Context) (<-chan ports.Snapshot, error) {
	return nil, fmt.Errorf("clipboard: watch: %w", ErrUnsupported)
}

// Write returns ErrUnsupported.
func (c *Clipboard) Write(_ context.Context, _ ports.Content) error {
	return fmt.Errorf("clipboard: write: %w", ErrUnsupported)
}

// WriteExcluded returns ErrUnsupported.
func (c *Clipboard) WriteExcluded(_ context.Context, _ ports.Content) error {
	return fmt.Errorf("clipboard: write: %w", ErrUnsupported)
}
