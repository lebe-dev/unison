package blob

import (
	"embed"

	"github.com/lebe-dev/unison/core/store"
)

// migrationsBase is the version block owned by the blob domain.
const migrationsBase = 400

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the schema migrations owned by this package.
func Migrations() ([]store.Migration, error) {
	return store.MigrationsFromFS(migrationsFS, "migrations", migrationsBase)
}
