package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lebe-dev/unison/core/ports/fake"
)

// openTestStore opens a store in a temporary directory with an in-memory
// keystore unless the options say otherwise.
func openTestStore(t *testing.T, opts Options) *Store {
	t.Helper()

	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	if opts.Keystore == nil {
		opts.Keystore = fake.NewKeystore()
	}
	s, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenCreatesDatabase(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})

	baseline, err := loadBaseline()
	if err != nil {
		t.Fatalf("load baseline migrations: %v", err)
	}
	want := baseline[len(baseline)-1].Version

	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != want {
		t.Fatalf("schema version: got %d, want %d", version, want)
	}

	var mode string
	if err := s.read.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode: got %q, want wal", mode)
	}

	var busy int64
	if err := s.write.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("read busy timeout: %v", err)
	}
	if busy != busyTimeout.Milliseconds() {
		t.Fatalf("busy timeout: got %d, want %d", busy, busyTimeout.Milliseconds())
	}

	if got, err := s.Settings().GetString(ctx, "absent", "default"); err != nil || got != "default" {
		t.Fatalf("read absent setting: got %q, %v", got, err)
	}
}

func TestOpenValidation(t *testing.T) {
	ctx := context.Background()

	if _, err := Open(ctx, Options{Keystore: fake.NewKeystore()}); err == nil {
		t.Fatal("open without data directory: want an error")
	}
	if _, err := Open(ctx, Options{Dir: t.TempDir()}); err == nil {
		t.Fatal("open without keystore: want an error")
	}
}

func TestMigrationsOnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keystore := fake.NewKeystore()

	peers := Migration{
		Version: 100,
		Name:    "100_peers",
		SQL:     "CREATE TABLE peers (device_id BLOB NOT NULL PRIMARY KEY) STRICT;",
	}
	history := Migration{
		Version: 200,
		Name:    "200_history",
		SQL: "CREATE TABLE history (id BLOB NOT NULL PRIMARY KEY, expires_at INTEGER NOT NULL);" +
			"CREATE INDEX history_expires_at ON history (expires_at);",
	}

	first := openTestStore(t, Options{Dir: dir, Keystore: keystore, Migrations: []Migration{peers}})
	if err := first.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO peers (device_id) VALUES (?)", []byte{1, 2, 3})
		return err
	}); err != nil {
		t.Fatalf("insert peer: %v", err)
	}
	if version, err := first.SchemaVersion(ctx); err != nil || version != 100 {
		t.Fatalf("schema version: got %d, %v, want 100", version, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	second := openTestStore(t, Options{Dir: dir, Keystore: keystore, Migrations: []Migration{peers}})
	if version, err := second.SchemaVersion(ctx); err != nil || version != 100 {
		t.Fatalf("schema version after reopen: got %d, %v, want 100", version, err)
	}
	var kept int64
	if err := second.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT count(*) FROM peers").Scan(&kept)
	}); err != nil {
		t.Fatalf("count peers: %v", err)
	}
	if kept != 1 {
		t.Fatalf("peers kept by reopen: got %d, want 1", kept)
	}

	third := openTestStore(t, Options{Dir: dir, Keystore: keystore, Migrations: []Migration{history, peers}})
	if version, err := third.SchemaVersion(ctx); err != nil || version != 200 {
		t.Fatalf("schema version after adding a migration: got %d, %v, want 200", version, err)
	}
	var tables int64
	if err := third.View(ctx, func(tx *sql.Tx) error {
		const query = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('peers', 'history')"
		return tx.QueryRowContext(ctx, query).Scan(&tables)
	}); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 2 {
		t.Fatalf("domain tables: got %d, want 2", tables)
	}
}

func TestMigrationsRejectInvalidSets(t *testing.T) {
	ctx := context.Background()
	keystore := fake.NewKeystore()

	tests := []struct {
		name       string
		migrations []Migration
	}{
		{
			name:       "duplicate version",
			migrations: []Migration{{Version: 100, Name: "a", SQL: "SELECT 1;"}, {Version: 100, Name: "b", SQL: "SELECT 1;"}},
		},
		{
			name:       "baseline version",
			migrations: []Migration{{Version: 1, Name: "a", SQL: "SELECT 1;"}},
		},
		{
			name:       "no name",
			migrations: []Migration{{Version: 100, SQL: "SELECT 1;"}},
		},
		{
			name:       "no sql",
			migrations: []Migration{{Version: 100, Name: "a"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := Options{Dir: t.TempDir(), Keystore: keystore, Migrations: test.migrations}
			if _, err := Open(ctx, opts); err == nil {
				t.Fatal("open: want an error")
			}
		})
	}
}

func TestMigrationsRejectNewerDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keystore := fake.NewKeystore()

	ahead := Migration{Version: 300, Name: "300_ahead", SQL: "CREATE TABLE ahead (v INTEGER) STRICT;"}
	s := openTestStore(t, Options{Dir: dir, Keystore: keystore, Migrations: []Migration{ahead}})
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	_, err := Open(ctx, Options{Dir: dir, Keystore: keystore})
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("open older code: got %v, want ErrSchemaNewer", err)
	}
}

func TestWithTxCommitAndRollback(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TABLE kept (v INTEGER NOT NULL) STRICT")
		return err
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	wantErr := errors.New("stop")
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO kept (v) VALUES (1)"); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("rollback: got %v, want %v", err, wantErr)
	}

	if err := s.View(ctx, func(tx *sql.Tx) error {
		var rows int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM kept").Scan(&rows); err != nil {
			return err
		}
		if rows != 0 {
			return fmt.Errorf("rolled back rows: got %d, want 0", rows)
		}
		return nil
	}); err != nil {
		t.Fatalf("view: %v", err)
	}
}

func TestViewIsReadOnly(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})

	err := s.View(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TABLE written (v INTEGER) STRICT")
		return err
	})
	if err == nil {
		t.Fatal("write in a read transaction: want an error")
	}
}

func TestConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TABLE counter (v INTEGER NOT NULL) STRICT")
		return err
	}); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const writers = 16
	var (
		wg   sync.WaitGroup
		errs = make(chan error, writers)
	)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.WithTx(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "INSERT INTO counter (v) VALUES (1)")
				return err
			})
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}

	if err := s.View(ctx, func(tx *sql.Tx) error {
		var rows int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM counter").Scan(&rows); err != nil {
			return err
		}
		if rows != writers {
			return fmt.Errorf("rows: got %d, want %d", rows, writers)
		}
		return nil
	}); err != nil {
		t.Fatalf("view: %v", err)
	}
}

func TestLostDEK(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keystore := fake.NewKeystore()

	s := openTestStore(t, Options{Dir: dir, Keystore: keystore})
	if err := s.Settings().SetString(ctx, "k", "v"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	if err := keystore.Delete(ctx, dekSecretName); err != nil {
		t.Fatalf("delete key: %v", err)
	}

	if _, err := Open(ctx, Options{Dir: dir, Keystore: keystore}); !errors.Is(err, ErrDEKMissing) {
		t.Fatalf("open without key: got %v, want ErrDEKMissing", err)
	}
}

func TestKeepsDEKAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keystore := fake.NewKeystore()

	s := openTestStore(t, Options{Dir: dir, Keystore: keystore})
	if err := s.Settings().SetString(ctx, "theme", "dark"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened := openTestStore(t, Options{Dir: dir, Keystore: keystore})
	if got, err := reopened.Settings().GetString(ctx, "theme", ""); err != nil || got != "dark" {
		t.Fatalf("setting after reopen: got %q, %v, want dark", got, err)
	}
}

func TestRejectsShortDEK(t *testing.T) {
	ctx := context.Background()
	keystore := fake.NewKeystore()
	if err := keystore.Store(ctx, dekSecretName, []byte("short")); err != nil {
		t.Fatalf("store key: %v", err)
	}

	if _, err := Open(ctx, Options{Dir: t.TempDir(), Keystore: keystore}); err == nil {
		t.Fatal("open with a short key: want an error")
	}
}
