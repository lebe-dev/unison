package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
)

// sink is a compile time check of the clipboard pipeline contract.
var _ clip.HistorySink = (*Store)(nil)

func TestAppendListAndGet(t *testing.T) {
	f := newFixture(t, testDevice(1))

	first := textItem(testID(1), f.origin, 1, "first", testTime, time.Hour)
	second := textItem(testID(2), f.origin, 2, "second", testTime.Add(time.Minute), time.Hour)
	f.append(first)
	f.append(second)

	items, err := f.store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("listed %d items, want 2", len(items))
	}
	if items[0].ID != second.ID || items[1].ID != first.ID {
		t.Fatalf("items are not newest first: %v", ids(items))
	}
	for _, item := range items {
		if !item.Complete {
			t.Errorf("item %s is not complete", item.ID)
		}
	}

	got, err := f.store.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if string(got.Representations[0].Inline) != "first" {
		t.Fatalf("item payload is %q", got.Representations[0].Inline)
	}

	if _, err := f.store.Get(context.Background(), testID(9)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item error is %v, want %v", err, ErrNotFound)
	}
}

func TestAppendKeepsExistingEntry(t *testing.T) {
	f := newFixture(t, testDevice(1))

	item := textItem(testID(1), f.origin, 1, "text", testTime, time.Hour)
	f.append(item)
	f.append(item)

	if items, err := f.store.List(context.Background(), ListFilter{}); err != nil || len(items) != 1 {
		t.Fatalf("listed %d items with error %v, want 1", len(items), err)
	}
	if got := f.count(itemsTable); got != 1 {
		t.Fatalf("%d rows, want 1", got)
	}
}

func TestListFilters(t *testing.T) {
	f := newFixture(t, testDevice(1))
	other := testDevice(2)

	text := textItem(testID(1), f.origin, 1, "text", testTime, time.Hour)
	image := blobItem(testID(2), other, 1, []byte("png"), testTime.Add(time.Minute), time.Hour)
	localImage := blobItem(testID(3), f.origin, 2, []byte("png"), testTime.Add(2*time.Minute), time.Hour)
	for _, item := range []model.ClipItem{text, image, localImage} {
		f.append(item)
	}

	ctx := context.Background()
	byKind, err := f.store.List(ctx, ListFilter{Kind: model.KindImage})
	if err != nil {
		t.Fatalf("list by kind: %v", err)
	}
	if len(byKind) != 2 {
		t.Fatalf("listed %d images, want 2", len(byKind))
	}

	byOrigin, err := f.store.List(ctx, ListFilter{Origin: other})
	if err != nil {
		t.Fatalf("list by device: %v", err)
	}
	if len(byOrigin) != 1 || byOrigin[0].ID != image.ID {
		t.Fatalf("listed %v for the other device, want %s", ids(byOrigin), image.ID)
	}

	limited, err := f.store.List(ctx, ListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("limit: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != localImage.ID {
		t.Fatalf("limited list is %v, want the newest item", ids(limited))
	}

	capped, err := f.store.List(ctx, ListFilter{Limit: MaxListLimit * 10})
	if err != nil {
		t.Fatalf("cap: %v", err)
	}
	if len(capped) != 3 {
		t.Fatalf("capped list has %d items, want 3", len(capped))
	}
}

func TestExpiryHidesAndCleanupRemoves(t *testing.T) {
	f := newFixture(t, testDevice(1))
	ctx := context.Background()

	data := []byte("image payload")
	short := blobItem(testID(1), f.origin, 1, data, testTime, time.Hour)
	long := blobItem(testID(2), f.origin, 2, data, testTime, 48*time.Hour)
	f.append(short)
	f.append(long)

	// Both items point at the same payload.
	if got := f.count(blobsTable); got != 1 {
		t.Fatalf("%d payload rows, want 1", got)
	}

	f.clock.Advance(2 * time.Hour)
	items, err := f.store.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].ID != long.ID {
		t.Fatalf("listed %v, want only the item that did not expire", ids(items))
	}

	removed, err := f.store.Cleanup(ctx)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d entries, want 1", removed)
	}
	if got := f.count(blobsTable); got != 1 {
		t.Fatalf("%d payload rows after the cleanup, want 1", got)
	}

	f.clock.Advance(48 * time.Hour)
	if _, err := f.store.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if got := f.count(itemsTable); got != 0 {
		t.Fatalf("%d rows after the cleanup, want none", got)
	}
	if got := f.count(blobsTable); got != 0 {
		t.Fatalf("%d payload rows after the cleanup, want none", got)
	}
}

func TestDeleteLeavesTombstone(t *testing.T) {
	f := newFixture(t, testDevice(1))
	ctx := context.Background()

	item := textItem(testID(1), f.origin, 1, "text", testTime, time.Hour)
	f.append(item)

	if err := f.store.Delete(ctx, item.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if items, err := f.store.List(ctx, ListFilter{}); err != nil || len(items) != 0 {
		t.Fatalf("listed %d items with error %v after the delete, want none", len(items), err)
	}
	if _, err := f.store.Get(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after the delete is %v, want %v", err, ErrNotFound)
	}
	if got := f.count(itemsTable); got != 2 {
		t.Fatalf("%d rows after the delete, want the item and its tombstone", got)
	}
	if err := f.store.Delete(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete is %v, want %v", err, ErrNotFound)
	}

	versions, err := f.store.Vector(ctx)
	if err != nil {
		t.Fatalf("vector: %v", err)
	}
	if versions[f.origin] != 2 {
		t.Fatalf("own version is %d, want 2", versions[f.origin])
	}

	f.clock.Advance(2 * time.Hour)
	if _, err := f.store.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if got := f.count(itemsTable); got != 0 {
		t.Fatalf("%d rows after the expiry, want none", got)
	}
}

func TestLastReturnsNewestLiveItem(t *testing.T) {
	f := newFixture(t, testDevice(1))

	if _, ok := f.store.Last(); ok {
		t.Fatal("empty history returned an item")
	}
	first := textItem(testID(1), f.origin, 1, "first", testTime, time.Hour)
	second := textItem(testID(2), f.origin, 2, "second", testTime.Add(time.Minute), time.Hour)
	f.append(first)
	f.append(second)

	got, ok := f.store.Last()
	if !ok || got.ID != second.ID {
		t.Fatalf("last item is %s, want %s", got.ID, second.ID)
	}
}

func TestValidate(t *testing.T) {
	f := newFixture(t, testDevice(1))
	valid := textItem(testID(1), f.origin, 1, "text", testTime, time.Hour)

	cases := map[string]func(model.ClipItem) model.ClipItem{
		"zero id":           func(item model.ClipItem) model.ClipItem { item.ID = model.ClipID{}; return item },
		"zero origin":       func(item model.ClipItem) model.ClipItem { item.Origin = model.DeviceID{}; return item },
		"zero sequence":     func(item model.ClipItem) model.ClipItem { item.Seq = 0; return item },
		"zero created time": func(item model.ClipItem) model.ClipItem { item.CreatedAt = time.Time{}; return item },
		"expiry before":     func(item model.ClipItem) model.ClipItem { item.ExpiresAt = item.CreatedAt; return item },
		"no hash": func(item model.ClipItem) model.ClipItem {
			item.Representations[0].Hash = [32]byte{}
			return item
		},
		"payload without hash": func(item model.ClipItem) model.ClipItem {
			item.Representations[0].Hash = [32]byte{}
			item.Representations[0].Inline = []byte("text")
			return item
		},
		"no mime": func(item model.ClipItem) model.ClipItem {
			item.Representations[0].MIME = ""
			return item
		},
	}
	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			if err := f.store.Append(context.Background(), broken(valid)); !errors.Is(err, ErrInvalidItem) {
				t.Fatalf("append is %v, want %v", err, ErrInvalidItem)
			}
		})
	}
}

func TestLoadTTLs(t *testing.T) {
	settings := settingsOf{
		SettingTextTTL: time.Hour,
	}
	ttls, err := LoadTTLs(context.Background(), settings)
	if err != nil {
		t.Fatalf("load ttls: %v", err)
	}
	if ttls.Text != time.Hour {
		t.Fatalf("text ttl is %v, want %v", ttls.Text, time.Hour)
	}
	if ttls.Image != DefaultImageTTL || ttls.Files != DefaultFilesTTL {
		t.Fatalf("image and file ttl are %v and %v, want the defaults", ttls.Image, ttls.Files)
	}
	if ttls.For(model.KindFiles) != DefaultFilesTTL {
		t.Fatalf("file ttl is %v", ttls.For(model.KindFiles))
	}
}

// settingsOf is a Settings implementation over a map.
type settingsOf map[string]time.Duration

// GetDuration returns the stored duration or def.
func (s settingsOf) GetDuration(_ context.Context, key string, def time.Duration) (time.Duration, error) {
	value, ok := s[key]
	if !ok {
		return def, nil
	}
	if value <= 0 {
		return def, errors.New("test: unusable duration")
	}
	return value, nil
}

func TestCopyWritesClipboard(t *testing.T) {
	f := newFixture(t, testDevice(1))
	ctx := context.Background()

	text := textItem(testID(1), f.origin, 1, "hello", testTime, time.Hour)
	f.append(text)
	if err := f.store.Copy(ctx, text.ID); err != nil {
		t.Fatalf("copy text: %v", err)
	}
	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("the clipboard did not receive the text")
	}
	if got := string(content.Representations[0].Data); got != "hello" {
		t.Fatalf("clipboard text is %q", got)
	}

	data := []byte("png payload")
	image := blobItem(testID(2), f.origin, 2, data, testTime, time.Hour)
	f.append(image)
	f.blobs.put(image.Representations[0].Hash, data)
	if err := f.store.Copy(ctx, image.ID); err != nil {
		t.Fatalf("copy image: %v", err)
	}
	content, ok = f.board.LastWrite()
	if !ok || content.Representations[0].MIME != "image/png" {
		t.Fatalf("the clipboard did not receive the image: %v", content)
	}
	if got := string(content.Representations[0].Data); got != string(data) {
		t.Fatalf("clipboard image is %q", got)
	}

	file := model.ClipItem{
		ID:        testID(3),
		Origin:    f.origin,
		Seq:       3,
		CreatedAt: testTime,
		ExpiresAt: testTime.Add(time.Hour),
		Kind:      model.KindFiles,
		Representations: []model.Representation{{
			MIME:     "application/x-unison-file",
			Size:     2,
			Hash:     [32]byte{1},
			FileName: "note.txt",
		}},
	}
	f.append(file)
	if err := f.store.Copy(ctx, file.ID); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("copy file is %v, want %v", err, ErrUnsupported)
	}
}

func TestFetchRestoresPayload(t *testing.T) {
	var downloaded [][32]byte
	blobs := newMemoryPayloads()

	f := newFixture(t, testDevice(1), func(opts *Options) {
		opts.Payloads = blobs
		opts.Fetch = func(_ context.Context, hash [32]byte) error {
			downloaded = append(downloaded, hash)
			return blobs.putIfMissing(hash, []byte("image payload"))
		}
	})

	data := []byte("image payload")
	image := blobItem(testID(1), f.origin, 1, data, testTime, time.Hour)
	f.append(image)

	items, err := f.store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].Complete {
		t.Fatalf("item is complete before the download")
	}

	path := filepath.Join(t.TempDir(), "image.png")
	item, written, err := f.store.Fetch(context.Background(), image.ID, path)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(downloaded) != 1 || downloaded[0] != image.Representations[0].Hash {
		t.Fatalf("downloaded %v, want the image payload", downloaded)
	}
	if !item.Complete {
		t.Fatal("item is not complete after the download")
	}
	if len(written) != 1 || written[0] != path {
		t.Fatalf("written files are %v, want %s", written, path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the written payload: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("written payload is %q", got)
	}
}

func TestFetchWritesText(t *testing.T) {
	f := newFixture(t, testDevice(1))

	item := textItem(testID(1), f.origin, 1, "written", testTime, time.Hour)
	f.append(item)

	dir := t.TempDir()
	_, written, err := f.store.Fetch(context.Background(), item.ID, dir)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("written files are %v", written)
	}
	got, err := os.ReadFile(written[0])
	if err != nil {
		t.Fatalf("read the written payload: %v", err)
	}
	if string(got) != "written" {
		t.Fatalf("written payload is %q", got)
	}
}

func TestFetchWithoutSource(t *testing.T) {
	f := newFixture(t, testDevice(1))

	image := blobItem(testID(1), f.origin, 1, []byte("image"), testTime, time.Hour)
	f.append(image)

	if _, _, err := f.store.Fetch(context.Background(), image.ID, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("fetch is %v, want %v", err, ErrUnavailable)
	}
}

// putIfMissing stores a payload and reports a failure for another one.
func (p *memoryPayloads) putIfMissing(hash [32]byte, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blobs[hash] = data
	return nil
}
