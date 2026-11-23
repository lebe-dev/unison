package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// clearEntry is the part of a live entry that a tombstone copies.
type clearEntry struct {
	id        model.ClipID
	expiresAt time.Time
	kind      model.Kind
	source    model.Source
}

// Clear records a tombstone for every live entry and returns their number. The
// tombstones reach the other Personal devices with the history exchange, so the
// entries disappear there too.
func (s *Store) Clear(ctx context.Context) (int, error) {
	now := s.clock.Now().UnixMilli()
	removed := 0

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		entries, err := liveEntries(ctx, tx, now)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		first, err := reserveSequences(ctx, tx, s.origin, len(entries))
		if err != nil {
			return err
		}
		data, err := proto.Marshal(&wirev1.ClipItem{})
		if err != nil {
			return fmt.Errorf("history: encode tombstone: %w", err)
		}

		const query = `
			INSERT INTO history_items (origin, seq, id, created_at, expires_at, kind, source, deleted, payload)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`
		for i, entry := range entries {
			seq := first + uint64(i)
			sealed, err := s.db.Sealer().Seal(itemsTable, rowKey(s.origin, seq), data)
			if err != nil {
				return fmt.Errorf("history: seal tombstone: %w", err)
			}
			if _, err := tx.ExecContext(ctx, query,
				s.origin[:],
				int64(seq),
				entry.id[:],
				now,
				entry.expiresAt.UnixMilli(),
				int64(entry.kind),
				int64(entry.source),
				sealed,
			); err != nil {
				return fmt.Errorf("history: write tombstone: %w", err)
			}
		}
		removed = len(entries)
		return recordVector(ctx, tx, s.origin, first+uint64(removed)-1)
	})
	if err != nil {
		return 0, err
	}
	if removed > 0 {
		s.log.Info("history cleared", "items", removed)
		s.changed()
	}
	return removed, nil
}

// liveEntries returns the entries a clear hides, oldest first.
func liveEntries(ctx context.Context, tx *sql.Tx, now int64) ([]clearEntry, error) {
	const query = `
		SELECT i.id, i.expires_at, i.kind, i.source
		FROM history_items AS i` + liveCondition + `
		ORDER BY i.created_at, i.seq`

	rows, err := tx.QueryContext(ctx, query, now, now)
	if err != nil {
		return nil, fmt.Errorf("history: read live entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []clearEntry
	for rows.Next() {
		var (
			entry   clearEntry
			id      []byte
			expires int64
			kind    int64
			source  int64
		)
		if err := rows.Scan(&id, &expires, &kind, &source); err != nil {
			return nil, fmt.Errorf("history: read live entry: %w", err)
		}
		if len(id) != len(entry.id) {
			return nil, fmt.Errorf("history: stored id is %d bytes", len(id))
		}
		copy(entry.id[:], id)
		entry.expiresAt = timeFromUnixMilli(expires)
		entry.kind = model.Kind(kind)
		entry.source = model.Source(source)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("history: read live entries: %w", err)
	}
	return entries, nil
}

// reserveSequences reserves n sequence numbers of origin and returns the first
// one.
func reserveSequences(ctx context.Context, tx *sql.Tx, origin model.DeviceID, n int) (uint64, error) {
	if n <= 0 {
		return 0, errors.New("history: no sequences to reserve")
	}
	const query = `
		INSERT INTO clip_seq (origin, seq) VALUES (?, ?)
		ON CONFLICT (origin) DO UPDATE SET seq = seq + ?
		RETURNING seq`

	var last int64
	if err := tx.QueryRowContext(ctx, query, origin[:], n, n).Scan(&last); err != nil {
		return 0, fmt.Errorf("history: allocate sequences: %w", err)
	}
	first := last - int64(n) + 1
	if first <= 0 {
		return 0, errors.New("history: allocated an invalid sequence")
	}
	return uint64(first), nil
}
