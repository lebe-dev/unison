package clip

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/lebe-dev/unison/core/store"
)

// migrationsBase is the version block owned by the clip domain.
const migrationsBase = 200

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the schema migrations owned by this package.
func Migrations() ([]store.Migration, error) {
	return store.MigrationsFromFS(migrationsFS, "migrations", migrationsBase)
}

// nextSeq allocates the next sequence number of this device and persists it, so
// numbers stay monotonic across restarts.
func (p *Pipeline) nextSeq(ctx context.Context) (uint64, error) {
	const query = `
		INSERT INTO clip_seq (origin, seq) VALUES (?, 1)
		ON CONFLICT (origin) DO UPDATE SET seq = seq + 1
		RETURNING seq`

	var seq int64
	err := p.db.WithTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, query, p.origin[:]).Scan(&seq)
	})
	if err != nil {
		return 0, fmt.Errorf("clip: allocate sequence: %w", err)
	}
	return uint64(seq), nil
}
