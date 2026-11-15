package history

import (
	"embed"

	"github.com/lebe-dev/unison/core/store"
)

// migrationsBase is the version block owned by the history domain. It sits
// above the blocks that were in use before this domain, so a database written
// by an older build still receives these migrations.
const migrationsBase = 500

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the schema migrations owned by this package.
func Migrations() ([]store.Migration, error) {
	return store.MigrationsFromFS(migrationsFS, "migrations", migrationsBase)
}
