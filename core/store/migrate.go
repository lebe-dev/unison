package store

import (
	"cmp"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// baselineDir is the directory of the migrations owned by this package.
const baselineDir = "migrations"

//go:embed migrations/*.sql
var baselineFS embed.FS

// loadBaseline reads the migrations owned by this package once.
var loadBaseline = sync.OnceValues(func() ([]Migration, error) {
	return MigrationsFromFS(baselineFS, baselineDir, 0)
})

// Migration is a single forward-only schema change.
type Migration struct {
	// Version orders migrations and is recorded in the database header.
	Version int64
	// Name identifies the migration in errors.
	Name string
	// SQL holds the statements to run in one transaction.
	SQL string
}

// MigrationsFromFS reads migrations from the SQL files in dir. Each file is
// named <version>_<name>.sql, where version is a positive number; base is added
// to it. A package that owns a domain takes its own block of versions above the
// ones used here, for example 100, 200 or 300.
func MigrationsFromFS(fsys fs.FS, dir string, base int64) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations in %s: %w", dir, err)
	}

	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, err := migrationVersion(name, base)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		data, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    strings.TrimSuffix(name, ".sql"),
			SQL:     string(data),
		})
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("no migrations in %s", dir)
	}
	return migrations, nil
}

// migrationVersion parses the version prefix of a migration file name.
func migrationVersion(name string, base int64) (int64, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, errors.New("name must be <version>_<name>.sql")
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("version %q is not a number", prefix)
	}
	if version < 1 {
		return 0, fmt.Errorf("version %d is not positive", version)
	}
	if base < 0 {
		return 0, fmt.Errorf("base %d is negative", base)
	}
	return base + version, nil
}

// migrationsTable records the version of every applied migration. A single
// version is not enough: each domain owns a block of versions, so a migration
// added to a lower block sorts below the ones already applied.
const migrationsTable = "schema_migrations"

// migrate applies every migration that the database has not recorded yet.
func (s *Store) migrate(ctx context.Context, extra []Migration) error {
	migrations, err := mergeMigrations(extra)
	if err != nil {
		return err
	}

	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	newest := migrations[len(migrations)-1].Version
	if current > newest {
		return fmt.Errorf("%w: database is at %d, migrations end at %d", ErrSchemaNewer, current, newest)
	}

	applied, legacy, err := s.appliedMigrations(ctx)
	if err != nil {
		return err
	}
	for version := range applied {
		if !slices.ContainsFunc(migrations, func(m Migration) bool { return m.Version == version }) {
			return fmt.Errorf("%w: database has migration %d, which is unknown", ErrSchemaNewer, version)
		}
	}

	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		if legacy && m.Version <= current {
			if err := s.repairMigration(ctx, m); err != nil {
				return err
			}
			continue
		}
		if err := s.applyMigration(ctx, m, max(current, m.Version)); err != nil {
			return err
		}
		current = max(current, m.Version)
	}
	return nil
}

// appliedMigrations returns the recorded migration versions. It reports a
// legacy database, one that predates the record and knows only its highest
// version, and creates the record for it.
func (s *Store) appliedMigrations(ctx context.Context) (map[int64]bool, bool, error) {
	var exists int64
	const lookup = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	if err := s.read.QueryRowContext(ctx, lookup, migrationsTable).Scan(&exists); err != nil {
		return nil, false, fmt.Errorf("look up applied migrations: %w", err)
	}
	if exists == 0 {
		current, err := s.SchemaVersion(ctx)
		if err != nil {
			return nil, false, err
		}
		create := "CREATE TABLE " + migrationsTable + " (version INTEGER NOT NULL PRIMARY KEY, name TEXT NOT NULL) STRICT"
		if _, err := s.write.ExecContext(ctx, create); err != nil {
			return nil, false, fmt.Errorf("create applied migrations: %w", err)
		}
		return map[int64]bool{}, current > 0, nil
	}

	rows, err := s.read.QueryContext(ctx, "SELECT version FROM "+migrationsTable)
	if err != nil {
		return nil, false, fmt.Errorf("read applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[int64]bool)
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, false, fmt.Errorf("read applied migrations: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read applied migrations: %w", err)
	}
	return applied, false, nil
}

// repairMigration handles a migration at or below the version of a legacy
// database, which may have skipped it. Migrations create schema objects, so
// one that fails was applied before and is only recorded; one that succeeds
// fills the gap.
func (s *Store) repairMigration(ctx context.Context, m Migration) error {
	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if err := s.applyMigration(ctx, m, current); err == nil {
		return nil
	}
	if err := s.recordMigration(ctx, m); err != nil {
		return fmt.Errorf("migration %s (%d): %w", m.Name, m.Version, err)
	}
	return nil
}

// applyMigration runs one migration, records it and sets the schema version.
func (s *Store) applyMigration(ctx context.Context, m Migration, version int64) error {
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("run: %w", err)
		}
		if err := insertMigration(ctx, tx, m); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			return fmt.Errorf("record version: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("migration %s (%d): %w", m.Name, m.Version, err)
	}
	return nil
}

// recordMigration records a migration without running it.
func (s *Store) recordMigration(ctx context.Context, m Migration) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		return insertMigration(ctx, tx, m)
	})
}

// insertMigration adds a migration to the record of applied ones.
func insertMigration(ctx context.Context, tx *sql.Tx, m Migration) error {
	query := "INSERT INTO " + migrationsTable + " (version, name) VALUES (?, ?)"
	if _, err := tx.ExecContext(ctx, query, m.Version, m.Name); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	return nil
}

// mergeMigrations returns the migrations of this package and extra in the
// order they must be applied.
func mergeMigrations(extra []Migration) ([]Migration, error) {
	base, err := loadBaseline()
	if err != nil {
		return nil, err
	}

	all := make([]Migration, 0, len(base)+len(extra))
	all = append(all, base...)
	all = append(all, extra...)
	slices.SortFunc(all, func(a, b Migration) int { return cmp.Compare(a.Version, b.Version) })

	for i, m := range all {
		if m.Name == "" {
			return nil, fmt.Errorf("migration with version %d has no name", m.Version)
		}
		if m.Version < 1 {
			return nil, fmt.Errorf("migration %s has version %d, want a positive one", m.Name, m.Version)
		}
		if strings.TrimSpace(m.SQL) == "" {
			return nil, fmt.Errorf("migration %s has no SQL", m.Name)
		}
		if i == 0 {
			continue
		}
		if previous := all[i-1]; previous.Version == m.Version {
			return nil, fmt.Errorf("migrations %s and %s share version %d", previous.Name, m.Name, m.Version)
		}
	}
	return all, nil
}
