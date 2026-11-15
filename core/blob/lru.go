package blob

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Pin marks a blob as in use, so eviction skips it. Calls nest and every Pin
// needs its own Unpin.
func (s *Store) Pin(hash [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pins[hash]++
}

// Unpin releases a pin taken by Pin.
func (s *Store) Unpin(hash [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pins[hash] <= 1 {
		delete(s.pins, hash)
		return
	}
	s.pins[hash]--
}

// Pinned reports whether a caller holds the blob.
func (s *Store) Pinned(hash [32]byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pins[hash] > 0
}

// Usage returns the total plaintext size of the stored blobs.
func (s *Store) Usage(ctx context.Context) (int64, error) {
	return s.storedSize(ctx)
}

// Evict removes complete blobs that no caller pinned, least recently used
// first, until the stored size fits the limit. It returns the number of removed
// blobs.
func (s *Store) Evict(ctx context.Context) (int, error) {
	return s.makeRoom(ctx, 0)
}

// makeRoom evicts blobs until need more bytes fit below the limit. Partial
// blobs are never evicted, because their transfer is in progress.
func (s *Store) makeRoom(ctx context.Context, need int64) (int, error) {
	limit, err := s.Limit(ctx)
	if err != nil {
		return 0, err
	}
	usage, err := s.storedSize(ctx)
	if err != nil {
		return 0, err
	}
	if usage+need <= limit {
		return 0, nil
	}

	list, err := s.lruList(ctx)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, entry := range list {
		if usage+need <= limit {
			break
		}
		if s.Pinned(entry.hash) {
			continue
		}
		if err := s.remove(ctx, entry.hash); err != nil {
			s.log.Warn("evicting blob failed", "hash", hashHex(entry.hash), "err", err)
			continue
		}
		usage -= entry.size
		removed++
		s.log.Info("evicted blob", "hash", hashHex(entry.hash), "size", entry.size)
	}

	if usage+need > limit {
		return removed, fmt.Errorf("%w: %d bytes used of %d, %d bytes needed", ErrNoSpace, usage, limit, need)
	}
	return removed, nil
}

// remove deletes the file of a blob and its metadata.
func (s *Store) remove(ctx context.Context, hash [32]byte) error {
	if err := s.deleteRecord(ctx, hash); err != nil {
		return err
	}
	if err := os.Remove(s.path(hash)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blob: remove %s: %w", hashHex(hash), err)
	}
	return nil
}
