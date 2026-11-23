package history

import (
	"context"
	"testing"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
)

// TestMigrationsReachAnOlderDatabase checks that a database written before the
// history existed receives its schema. The history block has to sit above the
// blocks of the other domains for that.
func TestMigrationsReachAnOlderDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keys := fake.NewKeystore()

	clipMigrations, err := clip.Migrations()
	if err != nil {
		t.Fatalf("read clip migrations: %v", err)
	}
	blobMigrations, err := blob.Migrations()
	if err != nil {
		t.Fatalf("read blob migrations: %v", err)
	}
	historyMigrations, err := Migrations()
	if err != nil {
		t.Fatalf("read history migrations: %v", err)
	}

	older := append(append([]store.Migration{}, clipMigrations...), blobMigrations...)
	db, err := store.Open(ctx, store.Options{Dir: dir, Keystore: keys, Migrations: older})
	if err != nil {
		t.Fatalf("open the older database: %v", err)
	}
	version, err := db.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("read the schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the older database: %v", err)
	}

	current := make([]store.Migration, 0, len(older)+len(historyMigrations))
	current = append(current, older...)
	current = append(current, historyMigrations...)
	db, err = store.Open(ctx, store.Options{Dir: dir, Keystore: keys, Migrations: current})
	if err != nil {
		t.Fatalf("open the current database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	newer, err := db.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("read the schema version: %v", err)
	}
	if newer <= version {
		t.Fatalf("the schema stayed at %d, the history migrations were not applied", newer)
	}

	hist, err := New(Options{Store: db, Origin: testDevice(1)})
	if err != nil {
		t.Fatalf("open the history: %v", err)
	}
	if _, err := hist.List(ctx, ListFilter{}); err != nil {
		t.Fatalf("the history schema is missing: %v", err)
	}
}
