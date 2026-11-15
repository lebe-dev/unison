package history

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

// Cleanup limits.
const (
	// cleanupBatch is the number of entries one cleanup step removes.
	cleanupBatch = 500
	// maxCleanupRounds bounds the steps of one cleanup call.
	maxCleanupRounds = 8
)

// Run removes expired entries until ctx is done. It runs once at the start.
func (s *Store) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		if _, err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("history cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Cleanup removes the entries whose time to live is over together with the
// payload references they held. It returns the number of removed entries.
func (s *Store) Cleanup(ctx context.Context) (int, error) {
	now := s.expiry()

	var removed int
	for range maxCleanupRounds {
		step, err := s.cleanupStep(ctx, now)
		if err != nil {
			return removed, err
		}
		removed += step
		if step < cleanupBatch {
			break
		}
	}
	if removed > 0 {
		s.log.Info("expired history entries removed", "items", removed)
	}
	return removed, nil
}

// cleanupStep removes up to cleanupBatch expired entries.
func (s *Store) cleanupStep(ctx context.Context, now int64) (int, error) {
	var removed int

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		const query = `
			SELECT rowid, origin, seq, payload FROM history_items
			WHERE expires_at <= ?
			ORDER BY expires_at
			LIMIT ?`

		rows, err := tx.QueryContext(ctx, query, now, cleanupBatch)
		if err != nil {
			return fmt.Errorf("history: read expired entries: %w", err)
		}
		defer func() { _ = rows.Close() }()

		rowids := make([]any, 0, cleanupBatch)
		hashes := make([][32]byte, 0, cleanupBatch)
		for rows.Next() {
			var (
				rowid  int64
				origin []byte
				seq    int64
				sealed []byte
			)
			if err := rows.Scan(&rowid, &origin, &seq, &sealed); err != nil {
				return fmt.Errorf("history: read expired entry: %w", err)
			}
			device, err := model.DeviceIDFromBytes(origin)
			if err != nil {
				return fmt.Errorf("history: stored origin: %w", err)
			}
			reps, err := s.openPayload(device, uint64(seq), sealed)
			if err != nil {
				return err
			}
			for _, rep := range reps {
				if len(rep.Inline) == 0 && rep.Hash != [32]byte{} {
					hashes = append(hashes, rep.Hash)
				}
			}
			rowids = append(rowids, rowid)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("history: read expired entries: %w", err)
		}
		if len(rowids) == 0 {
			return nil
		}

		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(rowids)), ",")
		if _, err := tx.ExecContext(ctx, `DELETE FROM history_items WHERE rowid IN (`+placeholders+`)`, rowids...); err != nil {
			return fmt.Errorf("history: remove expired entries: %w", err)
		}
		if err := releaseReferences(ctx, tx, hashes); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM history_blobs WHERE refs <= 0`); err != nil {
			return fmt.Errorf("history: remove unused payloads: %w", err)
		}
		removed = len(rowids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
