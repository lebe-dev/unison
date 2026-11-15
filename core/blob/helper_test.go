package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
)

// opens is an opened pair of a node database and a blob store.
type opened struct {
	blobs *Store
	db    *store.Store
}

// testDirs keeps the directories of a test store.
type testDirs struct {
	root  string
	db    string
	blobs string
}

// newTestStore opens a blob store on a fresh database.
func newTestStore(t *testing.T, limit int64) *Store {
	t.Helper()
	return newTestStoreAt(t, newTestDirs(t), fake.NewKeystore(), limit).blobs
}

// newTestDirs returns the directories of a test store.
func newTestDirs(t *testing.T) testDirs {
	t.Helper()
	root := t.TempDir()
	return testDirs{root: root, db: filepath.Join(root, "data"), blobs: filepath.Join(root, "data", "blobs")}
}

// newTestStoreAt opens a blob store on the directories with an existing
// keystore, which reopens the data of an earlier call.
func newTestStoreAt(t *testing.T, dirs testDirs, keystore ports.Keystore, limit int64) opened {
	t.Helper()
	ctx := context.Background()

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	db, err := store.Open(ctx, store.Options{Dir: dirs.db, Keystore: keystore, Migrations: migrations})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	blobs, err := New(Options{Dir: dirs.blobs, Store: db, Limit: limit})
	if err != nil {
		t.Fatalf("open blob store: %v", err)
	}
	return opened{blobs: blobs, db: db}
}

// randomBytes returns n random bytes.
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	return data
}

// mustPut stores data and returns its metadata.
func mustPut(t *testing.T, s *Store, data []byte) Info {
	t.Helper()
	info, err := s.Put(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("put %d bytes: %v", len(data), err)
	}
	return info
}
