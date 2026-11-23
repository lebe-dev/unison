// Package ports defines the platform interfaces the core depends on.
package ports

import (
	"context"
	"errors"
)

// ErrNotFound reports that a requested secret does not exist.
var ErrNotFound = errors.New("secret not found")

// Keystore stores small secrets such as key seeds and data encryption keys.
type Keystore interface {
	// Load returns the secret stored under name, or ErrNotFound.
	Load(ctx context.Context, name string) ([]byte, error)
	// Store replaces the secret stored under name.
	Store(ctx context.Context, name string, secret []byte) error
	// Delete removes the secret stored under name. Deleting a missing secret is not an error.
	Delete(ctx context.Context, name string) error
}
