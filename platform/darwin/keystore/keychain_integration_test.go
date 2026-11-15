//go:build darwin && cgo && keychaintest

package keystore_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/darwin/keystore"
)

// testKeystore writes to a service unique to one test and removes every item it
// created when the test ends.
type testKeystore struct {
	*keystore.Keystore
	names []string
}

func newTestKeystore(t *testing.T) *testKeystore {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("random service: %v", err)
	}
	k, err := keystore.New(fmt.Sprintf("app.unison.test.%x", suffix))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tk := &testKeystore{Keystore: k}
	t.Cleanup(func() {
		for _, name := range tk.names {
			if err := k.Delete(context.Background(), name); err != nil {
				t.Errorf("cleanup %q: %v", name, err)
			}
		}
	})
	return tk
}

func (tk *testKeystore) store(t *testing.T, name string, secret []byte) {
	t.Helper()
	if err := tk.Store(context.Background(), name, secret); err != nil {
		t.Fatalf("Store(%q): %v", name, err)
	}
	tk.names = append(tk.names, name)
}

func (tk *testKeystore) load(t *testing.T, name string) []byte {
	t.Helper()
	secret, err := tk.Load(context.Background(), name)
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	return secret
}

func TestLoadMissing(t *testing.T) {
	tk := newTestKeystore(t)
	_, err := tk.Load(context.Background(), "missing")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load error = %v, want ErrNotFound", err)
	}
}

func TestRoundTrip(t *testing.T) {
	tk := newTestKeystore(t)
	want := []byte{0x00, 0x01, 0x00, 0xff, 0xfe, 0x00}
	tk.store(t, "identity/ed25519-seed", want)

	if got := tk.load(t, "identity/ed25519-seed"); !bytes.Equal(got, want) {
		t.Fatalf("Load = %x, want %x", got, want)
	}
}

func TestStoreOverwrites(t *testing.T) {
	tk := newTestKeystore(t)
	tk.store(t, "store.dek", []byte("first"))

	second := bytes.Repeat([]byte{0x42}, 32)
	tk.store(t, "store.dek", second)
	if got := tk.load(t, "store.dek"); !bytes.Equal(got, second) {
		t.Fatalf("Load = %x, want %x", got, second)
	}
}

func TestDelete(t *testing.T) {
	tk := newTestKeystore(t)
	tk.store(t, "store.dek", []byte("secret"))

	if err := tk.Delete(context.Background(), "store.dek"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := tk.Load(context.Background(), "store.dek"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
	if err := tk.Delete(context.Background(), "store.dek"); err != nil {
		t.Fatalf("repeated Delete: %v", err)
	}
}

func TestServicesAreIsolated(t *testing.T) {
	tk := newTestKeystore(t)
	other := newTestKeystore(t)
	tk.store(t, "shared-name", []byte("mine"))

	if _, err := other.Load(context.Background(), "shared-name"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load from another service = %v, want ErrNotFound", err)
	}
}

func TestEmptySecret(t *testing.T) {
	tk := newTestKeystore(t)
	tk.store(t, "empty", []byte{})

	if got := tk.load(t, "empty"); len(got) != 0 {
		t.Fatalf("Load = %x, want empty", got)
	}
}

func TestLongSecret(t *testing.T) {
	tk := newTestKeystore(t)
	want := make([]byte, 4096)
	if _, err := rand.Read(want); err != nil {
		t.Fatalf("random secret: %v", err)
	}
	tk.store(t, "long", want)

	if got := tk.load(t, "long"); !bytes.Equal(got, want) {
		t.Fatal("Load returned a different secret")
	}
}
