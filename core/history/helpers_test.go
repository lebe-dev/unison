package history

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testTime is the start of the test clock.
var testTime = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

// testDevice returns a device ID built from one byte.
func testDevice(b byte) model.DeviceID {
	var id model.DeviceID
	copy(id[:], bytes.Repeat([]byte{b}, len(id)))
	return id
}

// testID returns a clip ID built from one byte.
func testID(b byte) model.ClipID {
	var id model.ClipID
	copy(id[:], bytes.Repeat([]byte{b}, len(id)))
	return id
}

// textItem returns a text item with one inline representation.
func textItem(id model.ClipID, origin model.DeviceID, seq uint64, text string, created time.Time, ttl time.Duration) model.ClipItem {
	data := []byte(text)
	return model.ClipItem{
		ID:        id,
		Origin:    origin,
		Seq:       seq,
		CreatedAt: created,
		ExpiresAt: created.Add(ttl),
		Kind:      model.KindText,
		Source:    model.SourceClipboard,
		Representations: []model.Representation{{
			MIME:   mimeTextPlain,
			Size:   uint64(len(data)),
			Hash:   blake3.Sum256(data),
			Inline: data,
		}},
	}
}

// blobItem returns an image item whose payload is a blob.
func blobItem(id model.ClipID, origin model.DeviceID, seq uint64, data []byte, created time.Time, ttl time.Duration) model.ClipItem {
	return model.ClipItem{
		ID:        id,
		Origin:    origin,
		Seq:       seq,
		CreatedAt: created,
		ExpiresAt: created.Add(ttl),
		Kind:      model.KindImage,
		Source:    model.SourceClipboard,
		Representations: []model.Representation{{
			MIME: "image/png",
			Size: uint64(len(data)),
			Hash: blake3.Sum256(data),
		}},
	}
}

// fixture is one device of a history test.
type fixture struct {
	t      *testing.T
	db     *store.Store
	store  *Store
	clock  *fake.Clock
	board  *fake.Clipboard
	blobs  *memoryPayloads
	origin model.DeviceID
}

// newFixture opens a database with the scheduled migrations and a history. The
// setup functions adjust the options before the history is created.
func newFixture(t *testing.T, origin model.DeviceID, setup ...func(*Options)) *fixture {
	t.Helper()

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	clipMigrations, err := clip.Migrations()
	if err != nil {
		t.Fatalf("read clip migrations: %v", err)
	}
	db, err := store.Open(context.Background(), store.Options{
		Dir:        t.TempDir(),
		Keystore:   fake.NewKeystore(),
		Migrations: append(migrations, clipMigrations...),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})

	clock := fake.NewClock(testTime)
	board := fake.NewClipboard()
	blobs := newMemoryPayloads()
	opts := Options{
		Store:     db,
		Origin:    origin,
		Payloads:  blobs,
		Clipboard: board,
		Clock:     clock,
	}
	for _, fn := range setup {
		fn(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("new history: %v", err)
	}
	return &fixture{t: t, db: db, store: s, clock: clock, board: board, blobs: blobs, origin: origin}
}

// count returns the number of rows of a table.
func (f *fixture) count(table string) int {
	f.t.Helper()

	var count int
	err := f.db.View(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count)
	})
	if err != nil {
		f.t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// append records an item or fails the test.
func (f *fixture) append(item model.ClipItem) {
	f.t.Helper()

	if err := f.store.Append(context.Background(), item); err != nil {
		f.t.Fatalf("append item: %v", err)
	}
}

// ids returns the IDs of the listed items.
func ids(items []Item) []model.ClipID {
	out := make([]model.ClipID, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

// memoryPayloads is a Payloads implementation over a map.
type memoryPayloads struct {
	mu    sync.Mutex
	blobs map[[32]byte][]byte
}

// newMemoryPayloads creates an empty payload store.
func newMemoryPayloads() *memoryPayloads {
	return &memoryPayloads{blobs: make(map[[32]byte][]byte)}
}

// Has reports whether the payload is stored.
func (p *memoryPayloads) Has(_ context.Context, hash [32]byte) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.blobs[hash]
	return ok, nil
}

// Open returns a reader of one stored payload.
func (p *memoryPayloads) Open(_ context.Context, hash [32]byte) (Payload, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, ok := p.blobs[hash]
	if !ok {
		return nil, errors.New("test: payload is missing")
	}
	return memoryPayload{Reader: bytes.NewReader(data)}, nil
}

// put stores a payload.
func (p *memoryPayloads) put(hash [32]byte, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blobs[hash] = data
}

// memoryPayload reads one payload of the test store.
type memoryPayload struct {
	*bytes.Reader
}

// Size returns the payload size.
func (p memoryPayload) Size() int64 {
	return p.Reader.Size()
}

// Close releases the payload.
func (p memoryPayload) Close() error {
	return nil
}

// peer is one side of a history exchange in the tests.
type peer struct {
	id      model.DeviceID
	role    model.Role
	deliver func(ctx context.Context, msg *wirev1.ControlMessage) error
}

// DeviceID returns the device of the peer.
func (p *peer) DeviceID() model.DeviceID {
	return p.id
}

// Role returns the role of the peer.
func (p *peer) Role() model.Role {
	return p.role
}

// Send hands the message to the other side.
func (p *peer) Send(ctx context.Context, msg *wirev1.ControlMessage) error {
	if p.deliver == nil {
		return nil
	}
	return p.deliver(ctx, msg)
}

// connect returns the peers that lead from a to b and from b to a.
func connect(t *testing.T, a, b *fixture) (toB, toA Sender) {
	t.Helper()

	// A peer is filled in after both sides exist.
	var peerA, peerB *peer
	peerB = &peer{id: b.origin, role: model.RolePersonal, deliver: func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return dispatch(ctx, b.store, peerA, msg)
	}}
	peerA = &peer{id: a.origin, role: model.RolePersonal, deliver: func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return dispatch(ctx, a.store, peerB, msg)
	}}
	return peerB, peerA
}

// dispatch hands one control message to the handlers of the store.
func dispatch(ctx context.Context, s *Store, from Sender, msg *wirev1.ControlMessage) error {
	switch body := msg.GetBody().(type) {
	case *wirev1.ControlMessage_HistorySync:
		return s.handleSync(ctx, from, body.HistorySync)
	case *wirev1.ControlMessage_HistoryBatch:
		return s.handleBatch(ctx, from, body.HistoryBatch)
	default:
		return nil
	}
}

// syncBoth runs both directions between a and b, the way two sessions exchange
// their vectors when they open, and waits until the entries are written.
func syncBoth(t *testing.T, a, b *fixture) {
	t.Helper()

	toB, toA := connect(t, a, b)
	ctx := context.Background()
	if err := a.store.Sync(ctx, toB); err != nil {
		t.Fatalf("sync a: %v", err)
	}
	if err := b.store.Sync(ctx, toA); err != nil {
		t.Fatalf("sync b: %v", err)
	}
	waitSending(t, a.store)
	waitSending(t, b.store)
}

// waitSending waits until the store stopped sending entries.
func waitSending(t *testing.T, s *Store) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		sending := len(s.sending)
		s.mu.Unlock()
		if sending == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the history exchange did not finish")
}
