package mobile

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lebe-dev/unison/core/ports"
)

// keyDir is the directory of the wrapped secrets inside the data directory.
const keyDir = "keys"

// maxSecretBytes bounds one secret.
const maxSecretBytes = 4096

// keystore stores the wrapped secrets in files. The wrapping itself is done by
// the platform keystore.
type keystore struct {
	dir      string
	platform Platform
}

var _ ports.Keystore = (*keystore)(nil)

// newKeystore creates a keystore inside dir.
func newKeystore(dir string, platform Platform) (*keystore, error) {
	path := filepath.Join(dir, keyDir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("mobile: create key directory: %w", err)
	}
	return &keystore{dir: path, platform: platform}, nil
}

// path returns the file of one secret. The name is hex encoded, so it cannot
// leave the directory.
func (k *keystore) path(name string) (string, error) {
	if name == "" {
		return "", errors.New("mobile: empty secret name")
	}
	if len(name) > 256 {
		return "", errors.New("mobile: secret name is too long")
	}
	return filepath.Join(k.dir, hex.EncodeToString([]byte(name))+".key"), nil
}

// Load returns the secret stored under name.
func (k *keystore) Load(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := k.path(name)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // The name is a hex encoded secret name inside the keystore directory.
	wrapped, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mobile: read secret: %w", err)
	}

	secret := unwrapSecret(k.platform, wrapped)
	if secret == nil {
		return nil, errors.New("mobile: keystore unwrap failed")
	}
	return secret, nil
}

// Store wraps and keeps secret under name.
func (k *keystore) Store(ctx context.Context, name string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := k.path(name)
	if err != nil {
		return err
	}
	if len(secret) > maxSecretBytes {
		return errors.New("mobile: secret is too large")
	}
	wrapped := wrapSecret(k.platform, secret)
	if wrapped == nil {
		return errors.New("mobile: keystore wrap failed")
	}
	return writeFile(path, wrapped)
}

// Delete removes the secret stored under name.
func (k *keystore) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := k.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("mobile: delete secret: %w", err)
	}
	return nil
}

// writeFile replaces path with data atomically.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("mobile: create secret file: %w", err)
	}
	name := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("mobile: set secret permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("mobile: write secret: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("mobile: sync secret: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("mobile: close secret: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("mobile: replace secret: %w", err)
	}
	return nil
}

// wrapSecret asks the platform to encrypt secret.
func wrapSecret(platform Platform, secret []byte) (out []byte) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	return platform.KeystoreWrap(secret)
}

// unwrapSecret asks the platform to decrypt blob.
func unwrapSecret(platform Platform, blob []byte) (out []byte) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	return platform.KeystoreUnwrap(blob)
}
