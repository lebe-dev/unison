package blob

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// record is a stored metadata row.
type record struct {
	info     Info
	received []byte
}

// record returns the metadata row of a blob.
func (s *Store) record(ctx context.Context, hash [32]byte) (record, error) {
	var (
		rec     record
		partial bool
	)
	const query = `SELECT size, chunks, partial, received FROM blob_meta WHERE hash = ?`
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, query, hash[:]).Scan(
			&rec.info.Size, &rec.info.Chunks, &partial, &rec.received,
		)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return record{}, fmt.Errorf("%w: %s", ErrNotFound, hashHex(hash))
	}
	if err != nil {
		return record{}, fmt.Errorf("blob: read metadata of %s: %w", hashHex(hash), err)
	}

	rec.info.Hash = hash
	rec.info.Partial = partial
	return rec, nil
}

// replaceRecord stores an empty metadata row for a complete blob.
func (s *Store) replaceRecord(ctx context.Context, info Info) error {
	const query = `
		INSERT INTO blob_meta (hash, size, chunks, partial, received, created_at, last_used_at)
		VALUES (?, ?, ?, 0, NULL, ?, ?)
		ON CONFLICT (hash) DO UPDATE SET
			size = excluded.size,
			chunks = excluded.chunks,
			partial = 0,
			received = NULL,
			last_used_at = excluded.last_used_at`

	now := s.now().UnixMilli()
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, info.Hash[:], info.Size, info.Chunks, now, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("blob: write metadata of %s: %w", hashHex(info.Hash), err)
	}
	return nil
}

// insertPartial stores a metadata row for a blob that starts receiving chunks.
func (s *Store) insertPartial(ctx context.Context, info Info) error {
	const query = `
		INSERT INTO blob_meta (hash, size, chunks, partial, received, created_at, last_used_at)
		VALUES (?, ?, ?, 1, ?, ?, ?)`

	bitmap := make([]byte, bitmapLen(info.Chunks))
	now := s.now().UnixMilli()
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, info.Hash[:], info.Size, info.Chunks, bitmap, now, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("blob: write metadata of %s: %w", hashHex(info.Hash), err)
	}
	return nil
}

// markChunk records that the chunk at index is stored.
func (s *Store) markChunk(ctx context.Context, hash [32]byte, index, chunks int) error {
	const update = `UPDATE blob_meta SET received = ? WHERE hash = ?`

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var bitmap []byte
		const query = `SELECT received FROM blob_meta WHERE hash = ? AND partial = 1`
		if err := tx.QueryRowContext(ctx, query, hash[:]).Scan(&bitmap); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrNotFound, hashHex(hash))
			}
			return err
		}
		if want := bitmapLen(chunks); len(bitmap) != want {
			return fmt.Errorf("%w: chunk bitmap of %s is %d bytes, want %d", ErrBadFormat, hashHex(hash), len(bitmap), want)
		}
		setBit(bitmap, index)
		if _, err := tx.ExecContext(ctx, update, bitmap, hash[:]); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("blob: record chunk %d of %s: %w", index, hashHex(hash), err)
	}
	return nil
}

// markComplete records that a blob received all its chunks.
func (s *Store) markComplete(ctx context.Context, hash [32]byte) error {
	const query = `UPDATE blob_meta SET partial = 0, received = NULL, last_used_at = ? WHERE hash = ?`

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query, s.now().UnixMilli(), hash[:])
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, hashHex(hash))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("blob: mark %s complete: %w", hashHex(hash), err)
	}
	return nil
}

// deleteRecord removes the metadata row of a blob.
func (s *Store) deleteRecord(ctx context.Context, hash [32]byte) error {
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM blob_meta WHERE hash = ?`, hash[:])
		return err
	})
	if err != nil {
		return fmt.Errorf("blob: delete metadata of %s: %w", hashHex(hash), err)
	}
	return nil
}

// touch records the last use of a blob.
func (s *Store) touch(ctx context.Context, hash [32]byte) error {
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE blob_meta SET last_used_at = ? WHERE hash = ?`, s.now().UnixMilli(), hash[:])
		return err
	})
	if err != nil {
		return fmt.Errorf("blob: touch %s: %w", hashHex(hash), err)
	}
	return nil
}

// storedSize returns the total plaintext size of the stored blobs.
func (s *Store) storedSize(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM blob_meta`).Scan(&total)
	})
	if err != nil {
		return 0, fmt.Errorf("blob: total size: %w", err)
	}
	return total, nil
}

// evictable returns the complete blobs, least recently used first.
type evictable struct {
	hash [32]byte
	size int64
}

// lruList returns the complete blobs ordered by their last use.
func (s *Store) lruList(ctx context.Context) ([]evictable, error) {
	list := make([]evictable, 0)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT hash, size FROM blob_meta WHERE partial = 0 ORDER BY last_used_at ASC, hash ASC`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				raw  []byte
				size int64
			)
			if err := rows.Scan(&raw, &size); err != nil {
				return err
			}
			if len(raw) != 32 {
				return fmt.Errorf("%w: hash is %d bytes", ErrBadFormat, len(raw))
			}
			var hash [32]byte
			copy(hash[:], raw)
			list = append(list, evictable{hash: hash, size: size})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("blob: list for eviction: %w", err)
	}
	return list, nil
}

// bitmapLen returns the length of a chunk bitmap for chunks chunks.
func bitmapLen(chunks int) int {
	return (chunks + 7) / 8
}

// bitSet reports whether bit i of the bitmap is set.
func bitSet(bitmap []byte, i int) bool {
	return bitmap[i/8]&(1<<(i%8)) != 0
}

// setBit sets bit i of the bitmap.
func setBit(bitmap []byte, i int) {
	bitmap[i/8] |= 1 << (i % 8)
}

// missingChunks returns the chunk indices that the bitmap does not hold.
func missingChunks(bitmap []byte, chunks int) []int {
	missing := make([]int, 0, chunks)
	for i := range chunks {
		if i/8 < len(bitmap) && bitSet(bitmap, i) {
			continue
		}
		missing = append(missing, i)
	}
	return missing
}
