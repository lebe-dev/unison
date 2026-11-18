package history

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

// Item is one history entry with the state of its payloads.
type Item struct {
	model.ClipItem
	// Complete reports whether every payload of the item is stored on this
	// device.
	Complete bool
}

// ListFilter selects history entries.
type ListFilter struct {
	// Kind limits the items to one kind. The zero kind lists every kind.
	Kind model.Kind
	// Origin limits the items to one device. The zero device lists every
	// device.
	Origin model.DeviceID
	// Limit is the number of items to return. DefaultListLimit is used when
	// zero and MaxListLimit caps it.
	Limit int
}

// List returns the live items that match the filter, newest first.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Item, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}

	query, args := s.listQuery(filter, limit)
	items, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	known := make(map[[32]byte]bool)
	for i := range items {
		complete, err := s.complete(ctx, items[i].ClipItem, known)
		if err != nil {
			return nil, err
		}
		items[i].Complete = complete
	}
	return items, nil
}

// Get returns one live item.
func (s *Store) Get(ctx context.Context, id model.ClipID) (Item, error) {
	if id.IsZero() {
		return Item{}, fmt.Errorf("%w: id is zero", ErrInvalidItem)
	}

	query := liveItems + ` AND i.id = ? ORDER BY i.created_at DESC LIMIT 1`
	now := s.expiry()
	items, err := s.query(ctx, query, now, now, id[:])
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, ErrNotFound
	}

	complete, err := s.complete(ctx, items[0].ClipItem, nil)
	if err != nil {
		return Item{}, err
	}
	items[0].Complete = complete
	return items[0], nil
}

// Delete records a tombstone for one item. The tombstone is an entry of this
// device and reaches the other devices with the history exchange.
func (s *Store) Delete(ctx context.Context, id model.ClipID) error {
	item, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	seq, err := s.nextSeq(ctx)
	if err != nil {
		return err
	}
	tombstone := model.ClipItem{
		ID:        item.ID,
		Origin:    s.origin,
		Seq:       seq,
		CreatedAt: s.clock.Now(),
		ExpiresAt: item.ExpiresAt,
		Kind:      item.Kind,
		Source:    item.Source,
		Deleted:   true,
	}
	if err := s.Append(ctx, tombstone); err != nil {
		return err
	}

	s.log.Info("history item deleted", "id", id.String(), "origin", item.Origin.String())
	return nil
}

// itemColumns selects the columns of one item row.
const itemColumns = `
	SELECT origin, seq, id, created_at, expires_at, kind, source, deleted, payload`

// liveCondition keeps the entries that are neither expired nor hidden by a
// tombstone. The two expiry times of the condition are the same.
const liveCondition = `
	WHERE i.deleted = 0
	  AND i.expires_at > ?
	  AND NOT EXISTS (
		SELECT 1 FROM history_items AS tombstone
		WHERE tombstone.id = i.id
		  AND tombstone.deleted = 1
		  AND tombstone.expires_at > ?
	  )`

// liveItems selects the live entries.
const liveItems = itemColumns + `
	FROM history_items AS i` + liveCondition

// listQuery builds the query and arguments of List.
func (s *Store) listQuery(filter ListFilter, limit int) (string, []any) {
	var query strings.Builder
	query.WriteString(liveItems)

	now := s.expiry()
	args := make([]any, 0, 5)
	args = append(args, now, now)

	if filter.Kind != model.KindUnspecified {
		query.WriteString(" AND i.kind = ?")
		args = append(args, int64(filter.Kind))
	}
	if !filter.Origin.IsZero() {
		query.WriteString(" AND i.origin = ?")
		args = append(args, filter.Origin[:])
	}
	query.WriteString(" ORDER BY i.created_at DESC, i.seq DESC LIMIT ?")
	args = append(args, limit)
	return query.String(), args
}

// query runs a select of items and opens their payloads.
func (s *Store) query(ctx context.Context, query string, args ...any) ([]Item, error) {
	var items []Item
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("history: read items: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			item, sealed, err := s.scan(rows)
			if err != nil {
				return err
			}
			reps, err := s.openPayload(item.Origin, item.Seq, sealed)
			if err != nil {
				return err
			}
			item.Representations = reps
			items = append(items, Item{ClipItem: item})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// scan reads one row of the item select.
func (s *Store) scan(rows *sql.Rows) (model.ClipItem, []byte, error) {
	var (
		item    model.ClipItem
		origin  []byte
		id      []byte
		created int64
		expires int64
		kind    int64
		source  int64
		deleted int
		sealed  []byte
	)
	err := rows.Scan(&origin, &item.Seq, &id, &created, &expires, &kind, &source, &deleted, &sealed)
	if err != nil {
		return model.ClipItem{}, nil, fmt.Errorf("history: read item: %w", err)
	}

	device, err := model.DeviceIDFromBytes(origin)
	if err != nil {
		return model.ClipItem{}, nil, fmt.Errorf("history: stored origin: %w", err)
	}
	item.Origin = device
	if len(id) != len(item.ID) {
		return model.ClipItem{}, nil, fmt.Errorf("history: stored id is %d bytes", len(id))
	}
	copy(item.ID[:], id)
	item.CreatedAt = timeFromUnixMilli(created)
	item.ExpiresAt = timeFromUnixMilli(expires)
	item.Kind = model.Kind(kind)
	item.Source = model.Source(source)
	item.Deleted = deleted == 1
	return item, sealed, nil
}

// complete reports whether every payload of the item is stored locally. The
// cache holds the answers of earlier calls within one query.
func (s *Store) complete(ctx context.Context, item model.ClipItem, cache map[[32]byte]bool) (bool, error) {
	for _, rep := range item.Representations {
		if len(rep.Inline) > 0 || rep.Hash == [32]byte{} {
			continue
		}
		present, ok := cache[rep.Hash]
		if !ok {
			var err error
			if present, err = s.present(ctx, rep.Hash); err != nil {
				return false, err
			}
			if cache != nil {
				cache[rep.Hash] = present
			}
		}
		if !present {
			return false, nil
		}
	}
	return true, nil
}

// expiry returns the current time in unix milliseconds.
func (s *Store) expiry() int64 {
	return s.clock.Now().UnixMilli()
}

// timeFromUnixMilli converts zero to the zero time.
func timeFromUnixMilli(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// MarkPresent records the local state of one payload in the cached column.
func (s *Store) MarkPresent(ctx context.Context, hash [32]byte, present bool) error {
	const query = `UPDATE history_blobs SET present = ? WHERE hash = ?`
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, boolInt(present), hash[:])
		return err
	})
	if err != nil {
		return fmt.Errorf("history: record payload state: %w", err)
	}
	return nil
}
