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

// migrate applies every migration that is newer than the recorded schema
// version.
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

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return err
		}
		current = m.Version
	}
	return nil
}

// applyMigration runs one migration and records its version.
func (s *Store) applyMigration(ctx context.Context, m Migration) error {
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("run: %w", err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.Version)); err != nil {
			return fmt.Errorf("record version: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("migration %s (%d): %w", m.Name, m.Version, err)
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
