package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	// The driver registers itself with database/sql on import.
	_ "modernc.org/sqlite"

	"github.com/lebe-dev/unison/core/ports"
)

// DatabaseFile is the name of the database file inside the data directory.
const DatabaseFile = "unison.db"

const (
	// maxReaders is the size of the connection pool used for reads.
	maxReaders = 8
	// busyTimeout bounds how long a statement waits for a lock.
	busyTimeout = 5 * time.Second
)

// Errors reported by the store.
var (
	// ErrDEKMissing reports that the database exists but its encryption key is
	// absent from the keystore.
	ErrDEKMissing = errors.New("store: data encryption key not found")
	// ErrSchemaNewer reports a database written by a newer version of the code.
	ErrSchemaNewer = errors.New("store: database schema is newer than the known migrations")
	// ErrDecrypt reports a value that cannot be decrypted with the key and the
	// bound table and row key.
	ErrDecrypt = errors.New("store: cannot decrypt value")
	// ErrCiphertext reports a value too short to hold a nonce and a tag.
	ErrCiphertext = errors.New("store: ciphertext is too short")
)

// Options configures Open.
type Options struct {
	// Dir is the data directory. The database file and other node data live
	// there and the directory is created when missing.
	Dir string
	// Keystore holds the data encryption key.
	Keystore ports.Keystore
	// Migrations are schema migrations owned by other packages.
	Migrations []Migration
}

// Store is the SQLite database of the node.
type Store struct {
	path   string
	write  *sql.DB
	read   *sql.DB
	sealer *Sealer
}

// Open opens the database in opts.Dir, creating it and its encryption key when
// missing, and applies the known migrations.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.Dir == "" {
		return nil, errors.New("store: data directory is required")
	}
	if opts.Keystore == nil {
		return nil, errors.New("store: keystore is required")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	path := filepath.Join(opts.Dir, DatabaseFile)
	key, err := loadDEK(ctx, opts.Keystore, path)
	if err != nil {
		return nil, err
	}
	sealer, err := newSealer(key)
	if err != nil {
		return nil, err
	}

	write, err := openDatabase(ctx, path, true)
	if err != nil {
		return nil, err
	}
	read, err := openDatabase(ctx, path, false)
	if err != nil {
		_ = write.Close()
		return nil, err
	}

	s := &Store{path: path, write: write, read: read, sealer: sealer}
	if err := s.migrate(ctx, opts.Migrations); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// openDatabase opens one connection pool on path. Only the write pool can
// modify the database, and it holds a single connection.
func openDatabase(ctx context.Context, path string, write bool) (*sql.DB, error) {
	query := url.Values{}
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(NORMAL)")

	pool := maxReaders
	if write {
		query.Set("_txlock", "immediate")
		pool = 1
	} else {
		query.Set("_query_only", "1")
	}

	dsn := url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(pool)
	db.SetMaxIdleConns(pool)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if !write {
		return db, nil
	}
	if err := checkWAL(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// checkWAL verifies that the database runs in WAL mode.
func checkWAL(ctx context.Context, db *sql.DB) error {
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("read journal mode: %w", err)
	}
	if mode != "wal" {
		return fmt.Errorf("store: journal mode is %q, want wal", mode)
	}
	return nil
}

// WithTx runs fn in a write transaction and rolls it back when fn returns an
// error. Write transactions are serialized.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// View runs fn in a read-only transaction.
func (s *Store) View(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	return fn(tx)
}

// Sealer returns the sealer of the data encryption key.
func (s *Store) Sealer() *Sealer {
	return s.sealer
}

// Settings returns the settings stored in the database.
func (s *Store) Settings() *Settings {
	return NewSettings(s.write, s.sealer)
}

// SchemaVersion returns the version of the applied migrations.
func (s *Store) SchemaVersion(ctx context.Context) (int64, error) {
	var version int64
	if err := s.read.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

// Path returns the path of the database file.
func (s *Store) Path() string {
	return s.path
}

// Close releases the database connections.
func (s *Store) Close() error {
	return errors.Join(s.read.Close(), s.write.Close())
}
