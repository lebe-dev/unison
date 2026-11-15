// Package fake provides in-memory implementations of the platform ports for tests.
package fake

import (
	"context"
	"slices"
	"sync"

	"github.com/lebe-dev/unison/core/ports"
)

// Keystore is an in-memory ports.Keystore.
type Keystore struct {
	mu      sync.Mutex
	secrets map[string][]byte
}

// NewKeystore creates an empty keystore.
func NewKeystore() *Keystore {
	return &Keystore{secrets: make(map[string][]byte)}
}

var _ ports.Keystore = (*Keystore)(nil)

// Load returns a copy of the secret stored under name.
func (k *Keystore) Load(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	secret, ok := k.secrets[name]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return slices.Clone(secret), nil
}

// Store keeps a copy of secret under name.
func (k *Keystore) Store(ctx context.Context, name string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.secrets[name] = slices.Clone(secret)
	return nil
}

// Delete removes the secret stored under name.
func (k *Keystore) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.secrets, name)
	return nil
}
