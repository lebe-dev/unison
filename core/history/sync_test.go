package history

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

func TestSyncConverges(t *testing.T) {
	a := newFixture(t, testDevice(1))
	b := newFixture(t, testDevice(2))

	// More items than one batch carries.
	for i := range 70 {
		a.append(textItem(testID(byte(i+1)), a.origin, uint64(i+1), fmt.Sprintf("text-%d", i),
			testTime.Add(time.Duration(i)*time.Second), DefaultTextTTL))
	}
	b.append(textItem(testID(200), b.origin, 1, "own", testTime, DefaultTextTTL))

	syncBoth(t, a, b)

	listA, err := a.store.List(context.Background(), ListFilter{Limit: MaxListLimit})
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	listB, err := b.store.List(context.Background(), ListFilter{Limit: MaxListLimit})
	if err != nil {
		t.Fatalf("list b: %v", err)
	}
	if len(listA) != 71 || len(listB) != 71 {
		t.Fatalf("listed %d and %d items, want 71", len(listA), len(listB))
	}
	if got := a.count(itemsTable); got != 71 {
		t.Fatalf("device a holds %d entries, want 71", got)
	}
	if got := b.count(itemsTable); got != 71 {
		t.Fatalf("device b holds %d entries, want 71", got)
	}

	vectorA, err := a.store.Vector(context.Background())
	if err != nil {
		t.Fatalf("vector a: %v", err)
	}
	vectorB, err := b.store.Vector(context.Background())
	if err != nil {
		t.Fatalf("vector b: %v", err)
	}
	if len(vectorA) != 2 || len(vectorB) != 2 {
		t.Fatalf("vectors hold %d and %d origins, want 2", len(vectorA), len(vectorB))
	}
	if vectorA[a.origin] != 70 || vectorA[b.origin] != 1 {
		t.Fatalf("vector of a is %v", vectorA)
	}
	if vectorB[a.origin] != 70 || vectorB[b.origin] != 1 {
		t.Fatalf("vector of b is %v", vectorB)
	}

	// A payload description survives the exchange.
	item, err := b.store.Get(context.Background(), testID(1))
	if err != nil {
		t.Fatalf("get the first item on b: %v", err)
	}
	if string(item.Representations[0].Inline) != "text-0" {
		t.Fatalf("payload on b is %q", item.Representations[0].Inline)
	}
}

func TestSyncSecondExchangeSendsNothing(t *testing.T) {
	a := newFixture(t, testDevice(1))
	b := newFixture(t, testDevice(2))

	a.append(textItem(testID(1), a.origin, 1, "text", testTime, DefaultTextTTL))
	syncBoth(t, a, b)
	syncBoth(t, a, b)

	if got := b.count(itemsTable); got != 1 {
		t.Fatalf("device b holds %d entries after two exchanges, want 1", got)
	}
}

func TestSyncTombstone(t *testing.T) {
	a := newFixture(t, testDevice(1))
	b := newFixture(t, testDevice(2))
	ctx := context.Background()

	item := textItem(testID(1), a.origin, 1, "text", testTime, DefaultTextTTL)
	a.append(item)
	syncBoth(t, a, b)
	if _, err := b.store.Get(ctx, item.ID); err != nil {
		t.Fatalf("device b did not receive the item: %v", err)
	}

	if err := b.store.Delete(ctx, item.ID); err != nil {
		t.Fatalf("delete on b: %v", err)
	}
	if _, err := b.store.Get(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("item on b after the delete is %v", err)
	}

	syncBoth(t, a, b)
	if _, err := a.store.Get(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("item on a after the exchange is %v, want %v", err, ErrNotFound)
	}
	if got := a.count(itemsTable); got != 2 {
		t.Fatalf("device a holds %d entries, want the item and its tombstone", got)
	}
}

// TestSyncClear checks that a clear hides the entries on the other device.
func TestSyncClear(t *testing.T) {
	a := newFixture(t, testDevice(1))
	b := newFixture(t, testDevice(2))
	ctx := context.Background()

	a.append(textItem(testID(1), a.origin, 1, "first", testTime, DefaultTextTTL))
	a.append(textItem(testID(2), a.origin, 2, "second", testTime.Add(time.Minute), DefaultTextTTL))
	b.append(textItem(testID(3), b.origin, 1, "own", testTime, DefaultTextTTL))
	syncBoth(t, a, b)

	removed, err := a.store.Clear(ctx)
	if err != nil {
		t.Fatalf("clear on a: %v", err)
	}
	if removed != 3 {
		t.Fatalf("cleared %d items on a, want 3", removed)
	}
	syncBoth(t, a, b)

	for _, f := range []*fixture{a, b} {
		list, err := f.store.List(ctx, ListFilter{Limit: MaxListLimit})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("device holds %d items after the clear, want none", len(list))
		}
	}
	if got := b.count(itemsTable); got != 6 {
		t.Fatalf("device b holds %d entries, want three items and three tombstones", got)
	}
}

func TestSyncTombstoneBeforeItem(t *testing.T) {
	a := newFixture(t, testDevice(1))
	ctx := context.Background()

	item := textItem(testID(1), a.origin, 1, "text", testTime, DefaultTextTTL)
	a.append(item)

	// The tombstone of another device arrives before the item it hides.
	tombstone := model.ClipItem{
		ID:        item.ID,
		Origin:    testDevice(2),
		Seq:       1,
		CreatedAt: testTime,
		ExpiresAt: item.ExpiresAt,
		Kind:      item.Kind,
		Deleted:   true,
	}
	a.append(tombstone)

	if _, err := a.store.Get(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("item is %v, want %v", err, ErrNotFound)
	}
	if items, err := a.store.List(ctx, ListFilter{}); err != nil || len(items) != 0 {
		t.Fatalf("listed %d items with error %v, want none", len(items), err)
	}
}

func TestSyncSkipsFriends(t *testing.T) {
	f := newFixture(t, testDevice(1))
	ctx := context.Background()

	item := textItem(testID(1), testDevice(2), 1, "text", testTime, DefaultTextTTL)
	sent := 0
	friend := &peer{id: testDevice(2), role: model.RoleFriends, deliver: func(context.Context, *wirev1.ControlMessage) error {
		sent++
		return nil
	}}

	if err := f.store.Sync(ctx, friend); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if sent != 0 {
		t.Fatalf("a friend received %d messages, want none", sent)
	}

	batch := batchMessage([]*wirev1.ClipItem{protoconv.ClipItemToProto(item)})
	if err := f.store.handleBatch(ctx, friend, batch.GetHistoryBatch()); err != nil {
		t.Fatalf("handle batch: %v", err)
	}
	if got := f.count(itemsTable); got != 0 {
		t.Fatalf("a friend recorded %d entries, want none", got)
	}
}

func TestSyncCutsOversizedBatch(t *testing.T) {
	f := newFixture(t, testDevice(1))
	ctx := context.Background()

	peer := &peer{id: testDevice(2), role: model.RolePersonal}
	items := make([]*wirev1.ClipItem, 0, MaxBatchItems+10)
	for i := range MaxBatchItems + 10 {
		item := textItem(testID(byte(i+1)), peer.id, uint64(i+1), "text", testTime, DefaultTextTTL)
		items = append(items, protoconv.ClipItemToProto(item))
	}

	if err := f.store.Sync(ctx, peer); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := f.store.handleBatch(ctx, peer, &wirev1.HistoryBatch{Items: items}); err != nil {
		t.Fatalf("handle batch: %v", err)
	}
	if got := f.count(itemsTable); got != MaxBatchItems {
		t.Fatalf("recorded %d entries, want %d", got, MaxBatchItems)
	}
}

func TestVectorFromProto(t *testing.T) {
	vector, err := vectorFromProto(map[string]uint64{
		testDevice(1).String(): 3,
		testDevice(2).String(): 0,
	})
	if err != nil {
		t.Fatalf("read vector: %v", err)
	}
	if len(vector) != 1 || vector[testDevice(1)] != 3 {
		t.Fatalf("vector is %v", vector)
	}

	if _, err := vectorFromProto(map[string]uint64{"not a device": 1}); err == nil {
		t.Fatal("an unusable device id was accepted")
	}
	if _, err := vectorFromProto(map[string]uint64{model.DeviceID{}.String(): 1}); err == nil {
		t.Fatal("a vector of an empty device was accepted")
	}

	large := make(map[string]uint64, maxVectorOrigins+10)
	for i := range maxVectorOrigins + 10 {
		large[fmt.Sprintf("%064x", i+1)] = 1
	}
	capped, err := vectorFromProto(large)
	if err != nil {
		t.Fatalf("read large vector: %v", err)
	}
	if len(capped) > maxVectorOrigins {
		t.Fatalf("vector holds %d origins, limit %d", len(capped), maxVectorOrigins)
	}
}

func TestSyncMessagesAreBounded(t *testing.T) {
	a := newFixture(t, testDevice(1))

	for i := range 80 {
		a.append(textItem(testID(byte(i+1)), a.origin, uint64(i+1), "text",
			testTime.Add(time.Duration(i)*time.Second), DefaultTextTTL))
	}

	var batches [][]*wirev1.ClipItem
	collector := &peer{id: testDevice(2), role: model.RolePersonal, deliver: func(_ context.Context, msg *wirev1.ControlMessage) error {
		if batch := msg.GetHistoryBatch(); batch != nil {
			batches = append(batches, batch.GetItems())
		}
		return nil
	}}
	if err := a.store.Sync(context.Background(), collector); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := a.store.handleSync(context.Background(), collector, &wirev1.HistorySync{}); err != nil {
		t.Fatalf("handle sync: %v", err)
	}
	waitSending(t, a.store)
	if len(batches) < 2 {
		t.Fatalf("sent %d batches, want more than one", len(batches))
	}

	var total int
	for _, batch := range batches {
		if len(batch) > MaxBatchItems {
			t.Fatalf("batch holds %d items, limit %d", len(batch), MaxBatchItems)
		}
		total += len(batch)
	}
	if total != 80 {
		t.Fatalf("sent %d items, want 80", total)
	}
}

func TestEntriesAfter(t *testing.T) {
	a := newFixture(t, testDevice(1))
	ctx := context.Background()

	for i := range 5 {
		a.append(textItem(testID(byte(i+1)), a.origin, uint64(i+1), "text",
			testTime.Add(time.Duration(i)*time.Second), DefaultTextTTL))
	}

	entries, err := a.store.entriesAfter(ctx, a.origin, 2, 10)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	got := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Seq)
	}
	if !slices.Equal(got, []uint64{3, 4, 5}) {
		t.Fatalf("entries after 2 are %v", got)
	}
}
