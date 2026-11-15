//go:build !windows

package keystore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOperationsUnsupported(t *testing.T) {
	dir := t.TempDir()
	k, err := New(dir, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	if _, err := k.Load(ctx, "store.dek"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Load = %v, want ErrUnsupported", err)
	}
	if err := k.Store(ctx, "store.dek", []byte("secret")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Store = %v, want ErrUnsupported", err)
	}
	if err := k.Delete(ctx, "store.dek"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Delete = %v, want ErrUnsupported", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "store.dek.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat = %v, want the secret to be absent", err)
	}
}
