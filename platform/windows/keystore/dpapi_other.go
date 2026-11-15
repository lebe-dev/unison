//go:build !windows

package keystore

import (
	"context"
	"fmt"
)

// Load returns ErrUnsupported.
func (k *Keystore) Load(_ context.Context, name string) ([]byte, error) {
	return nil, fmt.Errorf("keystore: load %q: %w", name, ErrUnsupported)
}

// Store returns ErrUnsupported.
func (k *Keystore) Store(_ context.Context, name string, _ []byte) error {
	return fmt.Errorf("keystore: store %q: %w", name, ErrUnsupported)
}

// Delete returns ErrUnsupported.
func (k *Keystore) Delete(_ context.Context, name string) error {
	return fmt.Errorf("keystore: delete %q: %w", name, ErrUnsupported)
}
