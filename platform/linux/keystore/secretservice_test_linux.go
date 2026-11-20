//go:build secretservicetest

package keystore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// Fixed names of the integration secrets, so repeated runs clean up after
// themselves.
const (
	integrationService = "app.unison.test.keystore"
	integrationSeed    = "probe/ed25519-seed"
	integrationKey     = "probe/store.dek"
)

// TestSecretServiceOnSession checks the round trip against the real keyring of
// the desktop session.
func TestSecretServiceOnSession(t *testing.T) {
	session, err := Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !session.Bus {
		t.Skip("the session has no D-Bus session bus")
	}
	if !session.Service {
		t.Fatal("the session bus has no Secret Service")
	}
	if session.Alias != aliasDefault && session.Alias != aliasLogin {
		t.Fatalf("the keyring has no collection, the alias is %q", session.Alias)
	}
	if session.Locked {
		t.Log("the collection is locked, the test may show a prompt")
	}

	k, err := Open(t.TempDir(), integrationService, Options{
		Backend:       BackendSecretService,
		PromptTimeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	cleanupSecrets(t, k)
	t.Cleanup(func() { cleanupSecrets(t, k) })

	if _, err := k.Load(ctx, integrationSeed); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load after cleanup = %v, want ErrNotFound", err)
	}
	if err := k.Store(ctx, integrationSeed, []byte("seed material")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	secret, err := k.Load(ctx, integrationSeed)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(secret) != "seed material" {
		t.Fatalf("loaded secret is %q, want %q", secret, "seed material")
	}

	// A second store replaces the item instead of adding another one.
	if err := k.Store(ctx, integrationSeed, []byte("new material")); err != nil {
		t.Fatalf("Store over an existing secret: %v", err)
	}
	items, err := k.secrets.search(ctx, integrationService, integrationSeed)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("the keyring holds %d items of the secret, want 1", len(items))
	}
	secret, err = k.Load(ctx, integrationSeed)
	if err != nil {
		t.Fatalf("Load after overwrite: %v", err)
	}
	if string(secret) != "new material" {
		t.Fatalf("loaded secret is %q, want %q", secret, "new material")
	}

	if err := k.Store(ctx, integrationKey, []byte("data key")); err != nil {
		t.Fatalf("Store of the second secret: %v", err)
	}
	if err := k.Delete(ctx, integrationSeed); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := k.Load(ctx, integrationSeed); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
	if secret, err := k.Load(ctx, integrationKey); err != nil || string(secret) != "data key" {
		t.Fatalf("Load of the other secret = %q, %v", secret, err)
	}
}

// cleanupSecrets removes the secrets of earlier runs.
func cleanupSecrets(t *testing.T, k *Keystore) {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{integrationSeed, integrationKey} {
		if err := k.Delete(ctx, name); err != nil {
			t.Fatalf("cleanup of %q: %v", name, err)
		}
	}
}
