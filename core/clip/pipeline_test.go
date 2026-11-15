package clip

import (
	"context"
	"testing"
	"time"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
)

// Devices used by the tests: the origin of captured items and a remote peer.
var (
	testOrigin = model.DeviceID{0x01, 0x02, 0x03}
	testPeer   = model.DeviceID{0x04, 0x05, 0x06}
)

// Fake clock steps used by the helpers.
const (
	step      = 10 * time.Millisecond
	settleFor = 50
)

// fixture runs a pipeline over a temporary database and a fake clipboard.
type fixture struct {
	t        *testing.T
	dir      string
	keys     *fake.Keystore
	board    *fake.Clipboard
	clock    *fake.Clock
	announce chan model.ClipItem
	setup    func(*Options)

	db     *store.Store
	pipe   *Pipeline
	cancel context.CancelFunc
	done   chan struct{}
}

// newFixture starts a pipeline with the default collaborators.
func newFixture(t *testing.T) *fixture {
	return newFixtureWith(t, nil)
}

// newFixtureWith starts a pipeline after setup adjusted its options.
func newFixtureWith(t *testing.T, setup func(*Options)) *fixture {
	t.Helper()

	f := &fixture{
		t:        t,
		dir:      t.TempDir(),
		keys:     fake.NewKeystore(),
		board:    fake.NewClipboard(),
		clock:    fake.NewClock(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)),
		announce: make(chan model.ClipItem, 16),
		setup:    setup,
	}
	t.Cleanup(f.stop)
	f.start()
	return f
}

// start opens the database and runs the pipeline.
func (f *fixture) start() {
	f.t.Helper()

	migrations, err := Migrations()
	if err != nil {
		f.t.Fatalf("read migrations: %v", err)
	}
	db, err := store.Open(context.Background(), store.Options{
		Dir:        f.dir,
		Keystore:   f.keys,
		Migrations: migrations,
	})
	if err != nil {
		f.t.Fatalf("open store: %v", err)
	}

	opts := Options{
		Clipboard: f.board,
		Store:     db,
		Origin:    testOrigin,
		Announcer: &recordingAnnouncer{items: f.announce},
		Clock:     f.clock,
	}
	if f.setup != nil {
		f.setup(&opts)
	}
	pipe, err := New(opts)
	if err != nil {
		_ = db.Close()
		f.t.Fatalf("new pipeline: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pipe.Run(ctx)
	}()
	// Give Run time to register its watcher before a test emits a snapshot.
	time.Sleep(10 * time.Millisecond)

	f.db, f.pipe, f.cancel, f.done = db, pipe, cancel, done
}

// stop cancels the pipeline and closes the database.
func (f *fixture) stop() {
	if f.cancel == nil {
		return
	}
	f.cancel()
	<-f.done
	if err := f.db.Close(); err != nil {
		f.t.Errorf("close store: %v", err)
	}
	f.cancel = nil
}

// emit delivers snapshot to the pipeline and lets it consume the change.
func (f *fixture) emit(snapshot ports.Snapshot) {
	f.board.Emit(snapshot)
	f.deliver()
}

// deliver gives the pipeline time to consume clipboard changes. The fake clock
// does not move, so no debounce timer fires.
func (f *fixture) deliver() {
	time.Sleep(10 * time.Millisecond)
}

// waitItem returns the next announced item, moving the fake clock past the
// debounce window.
func (f *fixture) waitItem() model.ClipItem {
	f.t.Helper()

	timeout := time.After(2 * time.Second)
	for {
		select {
		case item := <-f.announce:
			return item
		case <-timeout:
			f.t.Fatal("no item announced")
		default:
		}
		f.clock.Advance(step)
		time.Sleep(time.Millisecond)
	}
}

// expectNoItem fails when an item is announced while the clock moves past the
// debounce window.
func (f *fixture) expectNoItem() {
	f.t.Helper()

	for range settleFor {
		f.clock.Advance(step)
		time.Sleep(time.Millisecond)
	}
	select {
	case item := <-f.announce:
		f.t.Fatalf("unexpected item %s", item.ID)
	default:
	}
}

// recordingAnnouncer collects announced items.
type recordingAnnouncer struct {
	items chan model.ClipItem
}

func (a *recordingAnnouncer) Announce(_ context.Context, item model.ClipItem) error {
	a.items <- item
	return nil
}

// fixedPolicy returns the same decision for every item.
type fixedPolicy struct {
	decision Decision
	err      error
}

func (p fixedPolicy) Decide(context.Context, model.DeviceID, model.ClipItem) (Decision, error) {
	return p.decision, p.err
}

// textSnapshot builds a clipboard snapshot with the given text payloads.
func textSnapshot(plain, html string) ports.Snapshot {
	var reps []ports.Representation
	if plain != "" {
		reps = append(reps, ports.Representation{MIME: MIMETextPlain, Data: []byte(plain)})
	}
	if html != "" {
		reps = append(reps, ports.Representation{MIME: MIMETextHTML, Data: []byte(html)})
	}
	return ports.Snapshot{Content: ports.Content{Representations: reps}}
}

// textItem builds a text item as a peer would announce it.
func textItem(plain, html string) model.ClipItem {
	var reps []model.Representation
	if plain != "" {
		reps = append(reps, inlineRepresentation(MIMETextPlain, plain))
	}
	if html != "" {
		reps = append(reps, inlineRepresentation(MIMETextHTML, html))
	}
	created := time.Date(2026, 11, 15, 11, 0, 0, 0, time.UTC)
	return model.ClipItem{
		ID:              model.ClipID{0x07},
		Origin:          testPeer,
		Seq:             3,
		CreatedAt:       created,
		ExpiresAt:       created.Add(DefaultTextTTL),
		Kind:            model.KindText,
		Representations: reps,
		Source:          model.SourceClipboard,
	}
}

// inlineRepresentation builds an inline text representation.
func inlineRepresentation(mime, value string) model.Representation {
	data := []byte(value)
	return model.Representation{
		MIME:   mime,
		Size:   uint64(len(data)),
		Hash:   blake3.Sum256(data),
		Inline: data,
	}
}
