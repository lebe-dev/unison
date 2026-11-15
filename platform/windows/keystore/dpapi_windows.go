//go:build windows

package keystore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/lebe-dev/unison/core/ports"
)

// cryptProtectUIForbidden keeps DPAPI from showing a prompt.
const cryptProtectUIForbidden = 0x1

// Load returns the secret stored under name, or ports.ErrNotFound.
func (k *Keystore) Load(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := k.path(name)
	if err != nil {
		return nil, err
	}
	blob, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("keystore: load %q: %w", name, ports.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	secret, err := unprotect(blob, k.entropy)
	if err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	return secret, nil
}

// Store replaces the secret stored under name.
func (k *Keystore) Store(ctx context.Context, name string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := k.path(name)
	if err != nil {
		return err
	}
	blob, err := protect(secret, k.entropy)
	if err != nil {
		return fmt.Errorf("keystore: store %q: %w", name, err)
	}
	if err := writeFile(path, blob); err != nil {
		return fmt.Errorf("keystore: store %q: %w", name, err)
	}
	return nil
}

// Delete removes the secret stored under name. Deleting a missing secret is
// not an error.
func (k *Keystore) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := k.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("keystore: delete %q: %w", name, err)
	}
	return nil
}

// protect encrypts data with the master key of the current user.
func protect(data, entropy []byte) ([]byte, error) {
	var out windows.DataBlob
	err := windows.CryptProtectData(blobOf(data), nil, blobOf(entropy), 0, nil, cryptProtectUIForbidden, &out)
	runtime.KeepAlive(data)
	runtime.KeepAlive(entropy)
	if err != nil {
		return nil, fmt.Errorf("protect: %w", err)
	}
	return takeBlob(out), nil
}

// unprotect decrypts a blob protected for the current user.
func unprotect(blob, entropy []byte) ([]byte, error) {
	var out windows.DataBlob
	err := windows.CryptUnprotectData(blobOf(blob), nil, blobOf(entropy), 0, nil, cryptProtectUIForbidden, &out)
	runtime.KeepAlive(blob)
	runtime.KeepAlive(entropy)
	if err != nil {
		return nil, fmt.Errorf("unprotect: %w", err)
	}
	return takeBlob(out), nil
}

// blobOf returns a DPAPI blob that points at data, which the caller keeps alive
// for the duration of the call. Empty data gives an absent blob.
func blobOf(data []byte) *windows.DataBlob {
	if len(data) == 0 {
		return nil
	}
	return &windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

// takeBlob copies the returned blob into a Go slice and frees it.
func takeBlob(blob windows.DataBlob) []byte {
	if blob.Data == nil {
		return []byte{}
	}
	defer func() {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	}()
	out := make([]byte, blob.Size)
	copy(out, unsafe.Slice(blob.Data, blob.Size))
	return out
}
