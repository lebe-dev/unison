package blob

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports/fake"
)

func TestLRUEvictsTheLeastRecentlyUsed(t *testing.T) {
	ctx := context.Background()
	clock := fake.NewClock(time.Unix(0, 0))
	s := newTestStoreWithClock(t, 3*ChunkSize, clock)

	first := mustPut(t, s, randomBytes(t, ChunkSize))
	clock.Advance(time.Second)
	second := mustPut(t, s, randomBytes(t, ChunkSize))
	clock.Advance(time.Second)
	third := mustPut(t, s, randomBytes(t, ChunkSize))

	// The oldest blob becomes the most recently used one, and the newest one
	// cannot be evicted while a caller holds it.
	clock.Advance(time.Second)
	if _, err := s.Open(ctx, first.Hash); err != nil {
		t.Fatalf("open blob: %v", err)
	}
	s.Pin(third.Hash)

	clock.Advance(time.Second)
	fourth := mustPut(t, s, randomBytes(t, ChunkSize))

	for _, want := range []struct {
		info Info
		has  bool
	}{
		{first, true},
		{second, false},
		{third, true},
		{fourth, true},
	} {
		has, err := s.Has(ctx, want.info.Hash)
		if err != nil {
			t.Fatalf("has: %v", err)
		}
		if has != want.has {
			t.Fatalf("blob %s has = %v, want %v", hashHex(want.info.Hash), has, want.has)
		}
	}
	s.Unpin(third.Hash)
	if has, err := s.Has(ctx, third.Hash); err != nil || !has {
		t.Fatalf("unpinned blob has = %v, %v", has, err)
	}
}

func TestPinnedBlobsAreNotEvicted(t *testing.T) {
	ctx := context.Background()
	clock := fake.NewClock(time.Unix(0, 0))
	s := newTestStoreWithClock(t, 2*ChunkSize, clock)

	first := mustPut(t, s, randomBytes(t, ChunkSize))
	clock.Advance(time.Second)
	second := mustPut(t, s, randomBytes(t, ChunkSize))
	s.Pin(first.Hash)
	s.Pin(second.Hash)

	clock.Advance(time.Second)
	_, err := s.Put(ctx, bytes.NewReader(randomBytes(t, ChunkSize)), ChunkSize)
	if !errors.Is(err, ErrNoSpace) {
		t.Fatalf("put without space: %v, want ErrNoSpace", err)
	}
	for _, info := range []Info{first, second} {
		if has, err := s.Has(ctx, info.Hash); err != nil || !has {
			t.Fatalf("pinned blob %s has = %v, %v, want true", hashHex(info.Hash), has, err)
		}
	}

	s.Unpin(first.Hash)
	s.Unpin(second.Hash)
	s.Unpin(second.Hash)
	if s.Pinned(second.Hash) {
		t.Fatal("blob stays pinned after an unbalanced unpin")
	}
}

func TestPartialBlobsAreNotEvicted(t *testing.T) {
	ctx := context.Background()
	clock := fake.NewClock(time.Unix(0, 0))
	s := newTestStoreWithClock(t, 2*ChunkSize, clock)

	// The tree of the partial blob comes from a node that holds the content.
	data := randomBytes(t, ChunkSize)
	sender := newTestStore(t, DefaultLimitDesktop)
	wanted := mustPut(t, sender, data)
	tree, err := sender.Tree(ctx, wanted.Hash)
	if err != nil {
		t.Fatalf("read tree: %v", err)
	}

	stored := mustPut(t, s, randomBytes(t, ChunkSize))
	if _, err := s.CreatePartial(ctx, wanted.Hash, wanted.Size, tree); err != nil {
		t.Fatalf("create partial: %v", err)
	}

	// Both blobs fill the store, so only the complete one can be evicted.
	newest := mustPut(t, s, randomBytes(t, ChunkSize))
	if has, err := s.Has(ctx, stored.Hash); err != nil || has {
		t.Fatalf("evicted blob has = %v, %v, want false", has, err)
	}
	if stat, err := s.Stat(ctx, wanted.Hash); err != nil || !stat.Partial {
		t.Fatalf("partial blob stat = %+v, %v, want partial", stat, err)
	}

	// The partial blob outlives every complete blob again.
	last := mustPut(t, s, randomBytes(t, ChunkSize))
	if has, err := s.Has(ctx, newest.Hash); err != nil || has {
		t.Fatalf("evicted blob has = %v, %v, want false", has, err)
	}
	if has, err := s.Has(ctx, last.Hash); err != nil || !has {
		t.Fatalf("newest blob has = %v, %v, want true", has, err)
	}
	if missing, err := s.Missing(ctx, wanted.Hash); err != nil || len(missing) != wanted.Chunks {
		t.Fatalf("missing = %v, %v", missing, err)
	}
}

func TestSettingOverridesTheLimit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, ChunkSize)

	if limit, err := s.Limit(ctx); err != nil || limit != ChunkSize {
		t.Fatalf("limit = %d, %v, want %d", limit, err, ChunkSize)
	}
	if err := s.db.Settings().SetInt(ctx, SettingMaxSize, 2*ChunkSize); err != nil {
		t.Fatalf("set limit: %v", err)
	}
	if limit, err := s.Limit(ctx); err != nil || limit != 2*ChunkSize {
		t.Fatalf("limit = %d, %v, want %d", limit, err, 2*ChunkSize)
	}
}

func TestUsageAndEvict(t *testing.T) {
	ctx := context.Background()
	clock := fake.NewClock(time.Unix(0, 0))
	s := newTestStoreWithClock(t, 3*ChunkSize, clock)

	infos := make([]Info, 0, 3)
	for range 3 {
		infos = append(infos, mustPut(t, s, randomBytes(t, ChunkSize)))
		clock.Advance(time.Second)
	}
	if usage, err := s.Usage(ctx); err != nil || usage != 3*ChunkSize {
		t.Fatalf("usage = %d, %v, want %d", usage, err, 3*ChunkSize)
	}

	// A lower limit makes the two oldest blobs evictable.
	if err := s.db.Settings().SetInt(ctx, SettingMaxSize, ChunkSize); err != nil {
		t.Fatalf("set limit: %v", err)
	}
	if removed, err := s.Evict(ctx); err != nil || removed != 2 {
		t.Fatalf("evict = %d, %v, want 2 removed", removed, err)
	}
	if usage, err := s.Usage(ctx); err != nil || usage != ChunkSize {
		t.Fatalf("usage = %d, %v, want %d", usage, err, ChunkSize)
	}
	if has, err := s.Has(ctx, infos[2].Hash); err != nil || !has {
		t.Fatalf("newest blob has = %v, %v, want true", has, err)
	}
}

// newTestStoreWithClock opens a blob store with a fixed clock.
func newTestStoreWithClock(t *testing.T, limit int64, clock *fake.Clock) *Store {
	t.Helper()
	opened := newTestStoreAt(t, newTestDirs(t), fake.NewKeystore(), limit)
	s, err := New(Options{Dir: opened.blobs.Dir(), Store: opened.db, Limit: limit, Clock: clock})
	if err != nil {
		t.Fatalf("open blob store: %v", err)
	}
	return s
}
