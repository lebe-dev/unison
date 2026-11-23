package daemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/trust"
)

// discardLog returns a logger that drops every record.
func discardLog() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// seedStore writes a device name and a discovery key into the store of a data
// directory, which is what a paired device leaves behind when it stops.
func seedStore(t *testing.T, dataDir string, keystore *fake.Keystore, name string) []byte {
	t.Helper()

	ctx := context.Background()
	migrations, err := bootstrapMigrations()
	if err != nil {
		t.Fatalf("migrations: %v", err)
	}
	db, err := store.Open(ctx, store.Options{Dir: dataDir, Keystore: keystore, Migrations: migrations})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	if err := db.Settings().SetString(ctx, settingDeviceName, name); err != nil {
		t.Fatalf("store device name: %v", err)
	}
	trustStore, err := trust.Open(db)
	if err != nil {
		t.Fatalf("open trust store: %v", err)
	}
	key, err := trustStore.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("create discovery key: %v", err)
	}
	return key
}

// TestBootstrapReadsPairedDevice checks that a restart of a paired device
// finds the stored device name and the discovery key, so the device is
// announced with its group tag from the first announcement on.
func TestBootstrapReadsPairedDevice(t *testing.T) {
	dataDir := t.TempDir()
	keystore := fake.NewKeystore()
	want := seedStore(t, dataDir, keystore, "  Work Mac  ")

	name, key := bootstrap(context.Background(), dataDir, keystore, discardLog())
	if name != "Work Mac" {
		t.Errorf("name is %q, want %q", name, "Work Mac")
	}
	if !bytes.Equal(key, want) {
		t.Errorf("discovery key is %x, want %x", key, want)
	}
}

// TestBootstrapOfNewDevice checks that a fresh data directory has neither a
// name nor a discovery key, and that the lookup leaves the store closed so the
// node can open it.
func TestBootstrapOfNewDevice(t *testing.T) {
	dataDir := t.TempDir()
	keystore := fake.NewKeystore()

	name, key := bootstrap(context.Background(), dataDir, keystore, discardLog())
	if name != "" {
		t.Errorf("name is %q, want empty", name)
	}
	if key != nil {
		t.Errorf("discovery key is %x, want none", key)
	}

	// A second open succeeds only when the lookup released the database.
	seedStore(t, dataDir, keystore, "after")
}

// TestBootstrapOfUnreadableStore checks that a store that cannot be opened
// leaves the daemon with the defaults instead of stopping it.
func TestBootstrapOfUnreadableStore(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(dataDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}

	name, key := bootstrap(context.Background(), dataDir, fake.NewKeystore(), discardLog())
	if name != "" || key != nil {
		t.Errorf("bootstrap returned %q and %x, want nothing", name, key)
	}
}

// TestKeyProvider checks that the provider has no keys until a source is set
// and follows the source that replaces the bootstrap key.
func TestKeyProvider(t *testing.T) {
	ctx := context.Background()
	provider := &keyProvider{}

	if key, err := provider.get(ctx); key != nil || err != nil {
		t.Fatalf("key without a source is %x, %v, want none", key, err)
	}
	if keys, err := provider.keys(ctx); keys != nil || err != nil {
		t.Fatalf("keys without a source are %x, %v, want none", keys, err)
	}
	if keys, err := provider.friendsKeys(ctx); keys != nil || err != nil {
		t.Fatalf("friends keys without a source are %x, %v, want none", keys, err)
	}

	provider.use(func(context.Context) ([]byte, error) { return []byte("boot"), nil })
	if key, _ := provider.get(ctx); string(key) != "boot" {
		t.Fatalf("key is %q, want %q", key, "boot")
	}

	failed := errors.New("store closed")
	provider.use(func(context.Context) ([]byte, error) { return nil, failed })
	provider.useKeys(func(context.Context) ([][]byte, error) { return [][]byte{[]byte("a"), []byte("b")}, nil })
	provider.useFriends(func(context.Context) ([][]byte, error) { return [][]byte{[]byte("f")}, nil })

	if _, err := provider.get(ctx); !errors.Is(err, failed) {
		t.Fatalf("key error is %v, want %v", err, failed)
	}
	if keys, _ := provider.keys(ctx); len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
	if keys, _ := provider.friendsKeys(ctx); len(keys) != 1 || string(keys[0]) != "f" {
		t.Fatalf("friends keys are %q, want [f]", keys)
	}
}
