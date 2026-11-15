package trust

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
)

func TestDiscoveryKey(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	if _, err := ts.DiscoveryKey(ctx); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("read a missing discovery key: got %v, want ErrSecretNotFound", err)
	}

	key, err := ts.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure discovery key: %v", err)
	}
	if len(key) != secretSize {
		t.Fatalf("discovery key is %d bytes, want %d", len(key), secretSize)
	}

	again, err := ts.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure discovery key again: %v", err)
	}
	if !bytes.Equal(again, key) {
		t.Fatal("a second ensure generated a different key")
	}
	stored, err := ts.DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("read discovery key: %v", err)
	}
	if !bytes.Equal(stored, key) {
		t.Fatal("the stored key differs from the generated one")
	}

	rotated := bytes.Repeat([]byte{7}, secretSize)
	if err := ts.SetDiscoveryKey(ctx, rotated); err != nil {
		t.Fatalf("set discovery key: %v", err)
	}
	stored, err = ts.DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("read rotated key: %v", err)
	}
	if !bytes.Equal(stored, rotated) {
		t.Fatal("the rotated key was not stored")
	}

	if err := ts.SetDiscoveryKey(ctx, []byte("short")); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("set a short key: got %v, want ErrInvalidSecret", err)
	}
}

func TestRendezvousKeys(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	first := testPeer(t, 20, model.RolePersonal).Public.DeviceID
	second := testPeer(t, 21, model.RolePersonal).Public.DeviceID

	if _, err := ts.RendezvousKey(ctx, first); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("read a missing rendezvous key: got %v, want ErrSecretNotFound", err)
	}

	key := bytes.Repeat([]byte{3}, secretSize)
	if err := ts.SetRendezvousKey(ctx, first, key); err != nil {
		t.Fatalf("set rendezvous key: %v", err)
	}
	stored, err := ts.RendezvousKey(ctx, first)
	if err != nil {
		t.Fatalf("read rendezvous key: %v", err)
	}
	if !bytes.Equal(stored, key) {
		t.Fatal("the stored rendezvous key differs from the written one")
	}
	if _, err := ts.RendezvousKey(ctx, second); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("read the key of another peer: got %v, want ErrSecretNotFound", err)
	}

	if err := ts.SetRendezvousKey(ctx, model.DeviceID{}, key); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("set a key for a zero id: got %v, want ErrInvalidPeer", err)
	}
	if err := ts.SetRendezvousKey(ctx, first, nil); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("set an empty key: got %v, want ErrInvalidSecret", err)
	}
}

func TestSecretsAreSealedOnDisk(t *testing.T) {
	ctx := context.Background()
	ts, db := openTestStoreWithDB(t)

	key, err := ts.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure discovery key: %v", err)
	}

	var sealed []byte
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT value FROM group_secrets WHERE name = ?", discoveryKeyName).Scan(&sealed)
	}); err != nil {
		t.Fatalf("read secret column: %v", err)
	}
	if bytes.Contains(sealed, key) {
		t.Fatal("the secret column contains the key")
	}

	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	for _, file := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if bytes.Contains(data, key) {
			t.Fatalf("%s contains the discovery key", file)
		}
	}
}

func TestSelfRequireConfirmation(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	require, err := ts.SelfRequireConfirmation(ctx)
	if err != nil {
		t.Fatalf("read the default policy: %v", err)
	}
	if require {
		t.Fatal("confirmation is on by default")
	}

	if err := ts.SetSelfRequireConfirmation(ctx, true); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	require, err = ts.SelfRequireConfirmation(ctx)
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if !require {
		t.Fatal("confirmation is not stored")
	}

	if err := ts.SetSelfRequireConfirmation(ctx, false); err != nil {
		t.Fatalf("clear policy: %v", err)
	}
	require, err = ts.SelfRequireConfirmation(ctx)
	if err != nil {
		t.Fatalf("read cleared policy: %v", err)
	}
	if require {
		t.Fatal("confirmation is not cleared")
	}
}

func TestSecretsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keystore := fake.NewKeystore()

	open := func() (*Store, *store.Store) {
		t.Helper()
		migrations, err := Migrations()
		if err != nil {
			t.Fatalf("load migrations: %v", err)
		}
		db, err := store.Open(ctx, store.Options{Dir: dir, Keystore: keystore, Migrations: migrations})
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
		ts, err := Open(db)
		if err != nil {
			t.Fatalf("open trust store: %v", err)
		}
		return ts, db
	}

	first, db := open()
	key, err := first.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure discovery key: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	second, db := open()
	defer func() { _ = db.Close() }()
	stored, err := second.DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("read discovery key after reopen: %v", err)
	}
	if !bytes.Equal(stored, key) {
		t.Fatal("the discovery key changed across a reopen")
	}
}
