package clip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/internal/observability"
)

// Pipeline timings and limits.
const (
	// DefaultDebounce is how long changes are merged before an item is created.
	DefaultDebounce = 250 * time.Millisecond
	// DefaultEchoWindow is how long own writes are ignored by the watcher.
	DefaultEchoWindow = 2 * time.Second
	// DefaultTextTTL is the lifetime of a captured text item.
	DefaultTextTTL = 7 * 24 * time.Hour
	// DefaultImageTTL is the lifetime of a captured image item.
	DefaultImageTTL = 3 * 24 * time.Hour
	// DefaultFilesTTL is the lifetime of a captured file item.
	DefaultFilesTTL = 24 * time.Hour
	// MaxInlineText is the largest payload carried inside an item. Bigger
	// payloads are stored as blobs and announced by hash.
	MaxInlineText = 64 << 10
	// maxRepresentations bounds the representations of one item.
	maxRepresentations = 16
	// maxPayloadBytes bounds a payload the pipeline reads into memory.
	maxPayloadBytes = 64 << 20
	// captureQueue is the number of captures waiting to be hashed.
	captureQueue = 8
)

// MIME types the pipeline knows by name.
const (
	MIMETextPlain  = "text/plain"
	MIMETextHTML   = "text/html"
	MIMEImagePNG   = "image/png"
	MIMEUnisonFile = "application/x-unison-file"
)

// Errors reported by the pipeline.
var (
	// ErrUnsupportedContent reports content the pipeline cannot carry.
	ErrUnsupportedContent = errors.New("clip: unsupported content")
	// ErrPaused reports a local submit while clipboard synchronization is paused.
	ErrPaused = errors.New("clip: clipboard synchronization is paused")
	// ErrPayloadMissing reports a payload that is not stored and cannot be
	// fetched.
	ErrPayloadMissing = errors.New("clip: payload is not available")
)

// TTLs are the lifetimes of captured items, one per kind.
type TTLs struct {
	Text  time.Duration
	Image time.Duration
	Files time.Duration
}

// For returns the lifetime of an item of kind.
func (t TTLs) For(kind model.Kind) time.Duration {
	switch kind {
	case model.KindImage:
		return t.Image
	case model.KindFiles:
		return t.Files
	default:
		return t.Text
	}
}

// withDefaults fills the zero fields with the package defaults.
func (t TTLs) withDefaults() TTLs {
	if t.Text <= 0 {
		t.Text = DefaultTextTTL
	}
	if t.Image <= 0 {
		t.Image = DefaultImageTTL
	}
	if t.Files <= 0 {
		t.Files = DefaultFilesTTL
	}
	return t
}

// Options configures a Pipeline.
type Options struct {
	// Clipboard is the local clipboard.
	Clipboard ports.Clipboard
	// Store keeps the pause state and the sequence counter.
	Store *store.Store
	// Origin is this device, recorded in captured items.
	Origin model.DeviceID
	// Announcer receives captured items. A nil Announcer records them in the
	// history only.
	Announcer Announcer
	// Planner decides what happens to incoming items. Policy is used when nil.
	Planner Planner
	// Policy decides whether incoming items reach the clipboard. Elevates to a
	// Planner that applies items eagerly; AutoApply is used when both are nil.
	Policy PolicyDecider
	// History records items. A new MemoryHistory is used when nil.
	History HistorySink
	// Payloads keeps the payloads above the inline limit. Without it such
	// content is not captured.
	Payloads PayloadStore
	// Fetch downloads a payload from the peers. Without it an applied item has
	// to be stored locally already.
	Fetch Fetcher
	// Events receives incoming items that wait for the user. Without it only
	// the history is updated.
	Events EventSink
	// DownloadsDir receives the files of incoming items. DefaultDownloadsDir is
	// used when empty.
	DownloadsDir string
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// Logger receives pipeline events. Nothing is logged when nil.
	Logger *slog.Logger
	// Debounce is the merge window. DefaultDebounce is used when zero.
	Debounce time.Duration
	// EchoWindow is how long own writes are ignored. DefaultEchoWindow is used
	// when zero.
	EchoWindow time.Duration
	// TTLs are the lifetimes of captured items. The package defaults are used
	// for the zero fields.
	TTLs TTLs
	// StripTracking removes the advertising parameters of the URLs in text
	// items. The node enables it by default; a zero value keeps the content as
	// it is.
	StripTracking bool
}

// Pipeline moves clipboard content between the platform and the peers.
type Pipeline struct {
	clipboard ports.Clipboard
	db        *store.Store
	origin    model.DeviceID
	announcer Announcer
	planner   Planner
	history   HistorySink
	payloads  PayloadStore
	fetch     Fetcher
	events    EventSink
	downloads string
	clock     ports.Clock
	log       *slog.Logger
	debounce  time.Duration
	echo      *echoGuard

	mu   sync.RWMutex
	ttls TTLs
	// stripTracking is read on every capture and apply, so a change applies at
	// once.
	stripTracking bool
}

// New creates a pipeline. The pipeline does nothing until Run is called and
// items are passed to Apply.
func New(opts Options) (*Pipeline, error) {
	if opts.Clipboard == nil {
		return nil, errors.New("clip: clipboard is required")
	}
	if opts.Store == nil {
		return nil, errors.New("clip: store is required")
	}
	if opts.Origin.IsZero() {
		return nil, errors.New("clip: origin device is required")
	}

	p := &Pipeline{
		clipboard: opts.Clipboard,
		db:        opts.Store,
		origin:    opts.Origin,
		announcer: opts.Announcer,
		history:   opts.History,
		payloads:  opts.Payloads,
		fetch:     opts.Fetch,
		events:    opts.Events,
		downloads: opts.DownloadsDir,
		clock:     opts.Clock,
		log:       opts.Logger,
		debounce:  opts.Debounce,
		ttls:      opts.TTLs.withDefaults(),

		stripTracking: opts.StripTracking,
	}
	switch {
	case opts.Planner != nil:
		p.planner = opts.Planner
	case opts.Policy != nil:
		p.planner = policyPlanner{decider: opts.Policy}
	default:
		p.planner = policyPlanner{decider: AutoApply{}}
	}
	if p.history == nil {
		p.history = NewMemoryHistory()
	}
	if p.clock == nil {
		p.clock = ports.SystemClock
	}
	if p.log == nil {
		p.log = slog.New(slog.DiscardHandler)
	}
	if p.debounce <= 0 {
		p.debounce = DefaultDebounce
	}
	if p.downloads == "" {
		p.downloads = DefaultDownloadsDir()
	}

	echoWindow := opts.EchoWindow
	if echoWindow <= 0 {
		echoWindow = DefaultEchoWindow
	}
	p.echo = newEchoGuard(p.clock, echoWindow)
	return p, nil
}

// SetTTLs changes the lifetimes of captured items, so the next captured item
// uses them. Items already recorded keep their expiry.
func (p *Pipeline) SetTTLs(ttls TTLs) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ttls = ttls.withDefaults()
}

// TTLs returns the lifetimes of the items the pipeline captures now.
func (p *Pipeline) TTLs() TTLs {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ttls
}

// SetStripTracking enables or disables the removal of the advertising
// parameters of the URLs in text items. The next captured and applied item
// uses the new value.
func (p *Pipeline) SetStripTracking(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stripTracking = enabled
}

// StripTracking reports whether the pipeline removes the advertising
// parameters of the URLs in text items.
func (p *Pipeline) StripTracking() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stripTracking
}

// Run watches the clipboard and creates items until ctx is done. Nothing is
// created while the pipeline is paused, for sensitive content and for own
// writes.
func (p *Pipeline) Run(ctx context.Context) error {
	snapshots, err := p.clipboard.Watch(ctx)
	if err != nil {
		return fmt.Errorf("clip: watch clipboard: %w", err)
	}

	records := make(chan ports.Content, captureQueue)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer observability.Recover()
		p.recordQueue(ctx, records)
	}()

	p.consume(ctx, snapshots, records)
	close(records)
	<-done
	return nil
}

// recordQueue hashes the queued captures one after another.
func (p *Pipeline) recordQueue(ctx context.Context, records <-chan ports.Content) {
	for content := range records {
		if err := p.capture(ctx, content); err != nil {
			p.log.Warn("capture clipboard item failed", "error", err)
		}
	}
}

// consume turns snapshots into items. Changes closer than the debounce window
// are merged into one item.
func (p *Pipeline) consume(ctx context.Context, snapshots <-chan ports.Snapshot, records chan<- ports.Content) {
	var (
		pending ports.Content
		timer   <-chan time.Time
	)
	for {
		select {
		case <-ctx.Done():
			return
		case snapshot, ok := <-snapshots:
			if !ok {
				return
			}
			if snapshot.Sensitive {
				p.log.Info("skipped sensitive clipboard content")
				pending, timer = ports.Content{}, nil
				continue
			}
			if snapshot.OwnedByUs || p.echo.match(snapshot.Content) {
				p.log.Debug("skipped own clipboard content")
				continue
			}
			paused, err := p.pausedAt(ctx)
			if err != nil {
				p.log.Warn("read pause state failed", "error", err)
				continue
			}
			if paused {
				p.log.Debug("skipped clipboard content while paused")
				continue
			}
			pending = mergeContent(pending, snapshot.Content)
			timer = p.clock.After(p.debounce)
		case <-timer:
			timer = nil
			content := pending
			pending = ports.Content{}
			if hasFiles(content) {
				p.queueCapture(records, content)
				continue
			}
			if err := p.capture(ctx, content); err != nil {
				p.log.Warn("capture clipboard item failed", "error", err)
			}
		}
	}
}

// queueCapture hands content with files to the hashing queue, so the watcher is
// not held up while a large file is hashed.
func (p *Pipeline) queueCapture(records chan<- ports.Content, content ports.Content) {
	select {
	case records <- content:
	default:
		p.log.Warn("skipped clipboard files: the hashing queue is full")
	}
}

// capture creates an item from merged clipboard content and records it in the
// history before announcing it.
func (p *Pipeline) capture(ctx context.Context, content ports.Content) error {
	paused, err := p.pausedAt(ctx)
	if err != nil {
		return err
	}
	if paused {
		p.log.Debug("dropped clipboard content while paused")
		return nil
	}
	_, _, err = p.record(ctx, content)
	return err
}

// Submit records content copied on this device and announces it to the peers.
// It reports ErrPaused while the pipeline is paused and ErrUnsupportedContent
// for content it cannot carry.
func (p *Pipeline) Submit(ctx context.Context, content ports.Content) (model.ClipItem, error) {
	paused, err := p.pausedAt(ctx)
	if err != nil {
		return model.ClipItem{}, err
	}
	if paused {
		return model.ClipItem{}, ErrPaused
	}
	item, ok, err := p.record(ctx, content)
	if err != nil {
		return model.ClipItem{}, err
	}
	if !ok {
		return model.ClipItem{}, ErrUnsupportedContent
	}
	return item, nil
}

// record turns content into an item, stores it and announces it. The item is
// left out when the content has no payload the pipeline can carry.
func (p *Pipeline) record(ctx context.Context, content ports.Content) (model.ClipItem, bool, error) {
	content = p.cleanTracking(content)
	kind, payloads, err := p.classify(content)
	if err != nil {
		return model.ClipItem{}, false, err
	}
	if len(payloads) == 0 {
		p.log.Debug("skipped clipboard content without a supported payload")
		return model.ClipItem{}, false, nil
	}

	reps, err := p.attachPayloads(ctx, payloads)
	if err != nil {
		return model.ClipItem{}, false, err
	}
	item, err := p.newItem(ctx, kind, reps)
	if err != nil {
		return model.ClipItem{}, false, err
	}
	if err := p.history.Append(ctx, item); err != nil {
		return model.ClipItem{}, false, fmt.Errorf("clip: record item: %w", err)
	}
	if p.announcer == nil {
		return item, true, nil
	}
	if err := p.announcer.Announce(ctx, item); err != nil {
		return model.ClipItem{}, false, fmt.Errorf("clip: announce item: %w", err)
	}
	return item, true, nil
}

// newItem builds an item with the next sequence number of this device.
func (p *Pipeline) newItem(ctx context.Context, kind model.Kind, reps []model.Representation) (model.ClipItem, error) {
	seq, err := p.nextSeq(ctx)
	if err != nil {
		return model.ClipItem{}, err
	}
	id, err := newID()
	if err != nil {
		return model.ClipItem{}, err
	}
	now := p.clock.Now()
	return model.ClipItem{
		ID:              id,
		Origin:          p.origin,
		Seq:             seq,
		CreatedAt:       now,
		ExpiresAt:       now.Add(p.TTLs().For(kind)),
		Kind:            kind,
		Representations: reps,
		Source:          model.SourceClipboard,
	}, nil
}

// report delivers one event to the configured sink.
func (p *Pipeline) report(ctx context.Context, event Event) {
	if p.events == nil {
		return
	}
	p.events.Report(ctx, event)
}

// Last returns the most recent item recorded in the history.
func (p *Pipeline) Last() (model.ClipItem, bool) {
	return p.history.Last()
}
