package store

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	// dekSize is the length of the data encryption key.
	dekSize = 32
	// dekSecretName is the keystore entry holding the data encryption key.
	dekSecretName = "store.dek"
)

// Sealer encrypts and authenticates column values with XChaCha20-Poly1305.
type Sealer struct {
	aead cipher.AEAD
}

// newSealer creates a sealer for a 32 byte key.
func newSealer(key []byte) (*Sealer, error) {
	if len(key) != dekSize {
		return nil, fmt.Errorf("store: data encryption key is %d bytes, want %d", len(key), dekSize)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("store: create cipher: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext for a column of table in the row identified by
// rowKey. The result is the nonce followed by the ciphertext. A value sealed
// for one table and row key cannot be opened for another.
func (s *Sealer) Seal(table, rowKey string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("store: read nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, BindingAAD(table, rowKey)), nil
}

// Open decrypts a value produced by Seal for the same table and row key.
func (s *Sealer) Open(table, rowKey string, ciphertext []byte) ([]byte, error) {
	nonceSize := s.aead.NonceSize()
	if len(ciphertext) < nonceSize+s.aead.Overhead() {
		return nil, fmt.Errorf("%w: %d bytes for %s", ErrCiphertext, len(ciphertext), table)
	}
	plaintext, err := s.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], BindingAAD(table, rowKey))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDecrypt, table)
	}
	return plaintext, nil
}

// BindingAAD returns the additional authenticated data binding a sealed value
// to a table and row key.
func BindingAAD(table, rowKey string) []byte {
	return []byte(table + "\x00" + rowKey)
}

// loadDEK returns the data encryption key of the database at path, creating it
// on first use.
func loadDEK(ctx context.Context, keystore ports.Keystore, path string) ([]byte, error) {
	key, err := keystore.Load(ctx, dekSecretName)
	if err == nil {
		if len(key) != dekSize {
			return nil, fmt.Errorf("store: data encryption key is %d bytes, want %d", len(key), dekSize)
		}
		return key, nil
	}
	if !errors.Is(err, ports.ErrNotFound) {
		return nil, fmt.Errorf("store: load data encryption key: %w", err)
	}

	_, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		return nil, fmt.Errorf("%w: %s exists", ErrDEKMissing, path)
	case !errors.Is(statErr, os.ErrNotExist):
		return nil, fmt.Errorf("store: stat database: %w", statErr)
	}

	key = make([]byte, dekSize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("store: generate data encryption key: %w", err)
	}
	if err := keystore.Store(ctx, dekSecretName, key); err != nil {
		return nil, fmt.Errorf("store: save data encryption key: %w", err)
	}
	return key, nil
}
