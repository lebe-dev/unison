//go:build windows

package keystore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lebe-dev/unison/core/ports"
)

func newTestKeystore(t *testing.T) *Keystore {
	t.Helper()
	k, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return k
}

func TestStoreLoadDelete(t *testing.T) {
	k := newTestKeystore(t)
	ctx := context.Background()
	secret := []byte("0123456789abcdef0123456789abcdef")

	if err := k.Store(ctx, "store.dek", secret); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := k.Load(ctx, "store.dek")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatal("Load returned a different secret")
	}
	if err := k.Delete(ctx, "store.dek"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := k.Load(ctx, "store.dek"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
	if err := k.Delete(ctx, "store.dek"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestStoreWritesProtectedBlob(t *testing.T) {
	k := newTestKeystore(t)
	secret := []byte("plain text secret")
	if err := k.Store(context.Background(), "store.dek", secret); err != nil {
		t.Fatalf("Store: %v", err)
	}
	blob, err := os.ReadFile(filepath.Join(k.Dir(), "store.dek.bin"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Contains(blob, secret) {
		t.Fatal("stored blob contains the secret in plain text")
	}
}

func TestStoreNestedName(t *testing.T) {
	k := newTestKeystore(t)
	ctx := context.Background()
	secret := []byte("seed")

	if err := k.Store(ctx, "identity/ed25519-seed", secret); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(k.Dir(), "identity", "ed25519-seed.bin")); err != nil {
		t.Fatalf("Stat: %v", err)
	}
	got, err := k.Load(ctx, "identity/ed25519-seed")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatal("Load returned a different secret")
	}
}

func TestLoadMissing(t *testing.T) {
	k := newTestKeystore(t)
	if _, err := k.Load(context.Background(), "store.dek"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load = %v, want ErrNotFound", err)
	}
}

func TestStoreOverwrites(t *testing.T) {
	k := newTestKeystore(t)
	ctx := context.Background()

	if err := k.Store(ctx, "store.dek", []byte("first")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := k.Store(ctx, "store.dek", []byte("second")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := k.Load(ctx, "store.dek")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("Load = %q, want %q", got, "second")
	}
}

func TestEntropy(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	entropy := []byte("application entropy")
	k, err := New(dir, entropy)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := k.Store(ctx, "store.dek", []byte("secret")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := k.Load(ctx, "store.dek")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != "secret" {
		t.Fatalf("Load = %q, want %q", got, "secret")
	}

	other, err := New(dir, []byte("another entropy"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := other.Load(ctx, "store.dek"); err == nil {
		t.Fatal("Load with a different entropy returned no error")
	}
}

func TestInvalidNames(t *testing.T) {
	k := newTestKeystore(t)
	ctx := context.Background()
	names := []string{"../escape", `..\escape`, "/abs", "a//b", "con"}

	if err := k.Store(ctx, "../escape", []byte("secret")); err == nil {
		t.Fatal("Store with a traversal name returned no error")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(k.Dir()), "escape.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat = %v, want the traversal target to be absent", err)
	}
	for _, name := range names {
		if _, err := k.Load(ctx, name); err == nil {
			t.Errorf("Load(%q) returned no error", name)
		}
		if err := k.Store(ctx, name, []byte("secret")); err == nil {
			t.Errorf("Store(%q) returned no error", name)
		}
		if err := k.Delete(ctx, name); err == nil {
			t.Errorf("Delete(%q) returned no error", name)
		}
	}
}

func TestLoadCorruptedFile(t *testing.T) {
	k := newTestKeystore(t)
	path := filepath.Join(k.Dir(), "store.dek.bin")
	if err := os.MkdirAll(k.Dir(), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("not a protected blob"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := k.Load(context.Background(), "store.dek")
	if err == nil {
		t.Fatal("Load of a corrupted file returned no error")
	}
	if errors.Is(err, ports.ErrNotFound) {
		t.Fatal("Load of a corrupted file reported ErrNotFound")
	}
}

func TestCanceledContext(t *testing.T) {
	k := newTestKeystore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := k.Load(ctx, "canceled"); err == nil {
		t.Fatal("Load with canceled context returned no error")
	}
	if err := k.Store(ctx, "canceled", []byte("secret")); err == nil {
		t.Fatal("Store with canceled context returned no error")
	}
	if err := k.Delete(ctx, "canceled"); err == nil {
		t.Fatal("Delete with canceled context returned no error")
	}
}
