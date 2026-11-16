//go:build darwin && cgo && keychaintest

package keystore_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/darwin/keystore"
)

// The services and accounts are fixed, so repeated runs reuse the same items
// and the Keychain asks for access once instead of once per run.
const (
	testService  = "app.unison.test.keychain"
	otherService = "app.unison.test.isolation"
)

// testItems are the accounts the tests write.
var testItems = []string{
	"identity/ed25519-seed",
	"store.dek",
	"shared-name",
	"empty",
	"long",
}

// newTestKeystore returns the keystore of service with the accounts of earlier
// runs removed, and removes them again when the test ends.
func newTestKeystore(t *testing.T, service string) *keystore.Keystore {
	t.Helper()

	k, err := keystore.New(service)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = clearItems(k)
	if errors.Is(err, keystore.ErrNoKeychain) {
		t.Skipf("no keychain available: %v", err)
	}
	if err != nil {
		t.Fatalf("clear %q: %v", service, err)
	}
	t.Cleanup(func() {
		if err := clearItems(k); err != nil && !errors.Is(err, keystore.ErrNoKeychain) {
			t.Errorf("clear %q: %v", service, err)
		}
	})
	return k
}

// clearItems deletes the accounts of the tests. A keychain belongs to the user,
// so a failure is reported to the caller instead of ending the process.
func clearItems(k *keystore.Keystore) error {
	for _, name := range testItems {
		if err := k.Delete(context.Background(), name); err != nil {
			return err
		}
	}
	return nil
}

// mustStore writes a secret and fails the test on error.
func mustStore(t *testing.T, k *keystore.Keystore, name string, secret []byte) {
	t.Helper()

	if err := k.Store(context.Background(), name, secret); err != nil {
		t.Fatalf("Store(%q): %v", name, err)
	}
}

// mustLoad reads a secret and fails the test on error.
func mustLoad(t *testing.T, k *keystore.Keystore, name string) []byte {
	t.Helper()

	secret, err := k.Load(context.Background(), name)
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	return secret
}

func TestLoadMissing(t *testing.T) {
	k := newTestKeystore(t, testService)
	if _, err := k.Load(context.Background(), "missing"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load error = %v, want ErrNotFound", err)
	}
}

func TestRoundTrip(t *testing.T) {
	k := newTestKeystore(t, testService)
	want := []byte{0x00, 0x01, 0x00, 0xff, 0xfe, 0x00}
	mustStore(t, k, "identity/ed25519-seed", want)

	if got := mustLoad(t, k, "identity/ed25519-seed"); !bytes.Equal(got, want) {
		t.Fatalf("Load = %x, want %x", got, want)
	}
}

func TestStoreOverwrites(t *testing.T) {
	k := newTestKeystore(t, testService)
	mustStore(t, k, "store.dek", []byte("first"))

	second := bytes.Repeat([]byte{0x42}, 32)
	mustStore(t, k, "store.dek", second)
	if got := mustLoad(t, k, "store.dek"); !bytes.Equal(got, second) {
		t.Fatalf("Load = %x, want %x", got, second)
	}
}

func TestDelete(t *testing.T) {
	k := newTestKeystore(t, testService)
	mustStore(t, k, "store.dek", []byte("secret"))

	if err := k.Delete(context.Background(), "store.dek"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := k.Load(context.Background(), "store.dek"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
	if err := k.Delete(context.Background(), "store.dek"); err != nil {
		t.Fatalf("repeated Delete: %v", err)
	}
}

func TestServicesAreIsolated(t *testing.T) {
	k := newTestKeystore(t, testService)
	other := newTestKeystore(t, otherService)
	mustStore(t, k, "shared-name", []byte("mine"))

	if _, err := other.Load(context.Background(), "shared-name"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load from another service = %v, want ErrNotFound", err)
	}
}

func TestEmptySecret(t *testing.T) {
	k := newTestKeystore(t, testService)
	mustStore(t, k, "empty", []byte{})

	if got := mustLoad(t, k, "empty"); len(got) != 0 {
		t.Fatalf("Load = %x, want empty", got)
	}
}

func TestLongSecret(t *testing.T) {
	k := newTestKeystore(t, testService)
	want := make([]byte, 4096)
	if _, err := rand.Read(want); err != nil {
		t.Fatalf("random secret: %v", err)
	}
	mustStore(t, k, "long", want)

	if got := mustLoad(t, k, "long"); !bytes.Equal(got, want) {
		t.Fatal("Load returned a different secret")
	}
}
