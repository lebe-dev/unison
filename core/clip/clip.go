package clip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/store"
)

// Pipeline timings and limits.
const (
	// DefaultDebounce is how long changes are merged before an item is created.
	DefaultDebounce = 250 * time.Millisecond
	// DefaultEchoWindow is how long own writes are ignored by the watcher.
	DefaultEchoWindow = 2 * time.Second
	// DefaultTextTTL is the lifetime of a captured text item.
	DefaultTextTTL = 7 * 24 * time.Hour
	// MaxInlineText is the largest text payload carried inside an item.
	MaxInlineText = 64 << 10
)

// MIME types the text pipeline accepts.
const (
	MIMETextPlain = "text/plain"
	MIMETextHTML  = "text/html"
)

// ErrUnsupportedContent reports content the text pipeline cannot carry.
var ErrUnsupportedContent = errors.New("clip: unsupported content")

// ErrPaused reports a local submit while clipboard synchronization is paused.
var ErrPaused = errors.New("clip: clipboard synchronization is paused")

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
	// Policy decides whether incoming items reach the clipboard. AutoApply is
	// used when nil.
	Policy PolicyDecider
	// History records items. A new MemoryHistory is used when nil.
	History HistorySink
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// Logger receives pipeline events. Nothing is logged when nil.
	Logger *slog.Logger
	// Debounce is the merge window. DefaultDebounce is used when zero.
	Debounce time.Duration
	// EchoWindow is how long own writes are ignored. DefaultEchoWindow is used
	// when zero.
	EchoWindow time.Duration
	// TextTTL is the lifetime of a captured text item. DefaultTextTTL is used
	// when zero.
	TextTTL time.Duration
}

// Pipeline moves clipboard content between the platform and the peers.
type Pipeline struct {
	clipboard ports.Clipboard
	db        *store.Store
	origin    model.DeviceID
	announcer Announcer
	policy    PolicyDecider
	history   HistorySink
	clock     ports.Clock
	log       *slog.Logger
	debounce  time.Duration
	ttl       time.Duration
	echo      *echoGuard
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
		policy:    opts.Policy,
		history:   opts.History,
		clock:     opts.Clock,
		log:       opts.Logger,
		debounce:  opts.Debounce,
		ttl:       opts.TextTTL,
	}
	if p.policy == nil {
		p.policy = AutoApply{}
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
	if p.ttl <= 0 {
		p.ttl = DefaultTextTTL
	}

	echoWindow := opts.EchoWindow
	if echoWindow <= 0 {
		echoWindow = DefaultEchoWindow
	}
	p.echo = newEchoGuard(p.clock, echoWindow)
	return p, nil
}

// Run watches the clipboard and creates items until ctx is done. Nothing is
// created while the pipeline is paused, for sensitive content and for own
// writes.
func (p *Pipeline) Run(ctx context.Context) error {
	snapshots, err := p.clipboard.Watch(ctx)
	if err != nil {
		return fmt.Errorf("clip: watch clipboard: %w", err)
	}
	p.consume(ctx, snapshots)
	return nil
}

// consume turns snapshots into items. Changes closer than the debounce window
// are merged into one item.
func (p *Pipeline) consume(ctx context.Context, snapshots <-chan ports.Snapshot) {
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
				p.log.Warn("read pause state failed", "err", err)
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
			if err := p.capture(ctx, content); err != nil {
				p.log.Warn("capture clipboard item failed", "err", err)
			}
		}
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
// for content without text.
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
// left out when the content has no text to carry.
func (p *Pipeline) record(ctx context.Context, content ports.Content) (model.ClipItem, bool, error) {
	reps := p.textRepresentations(content)
	if len(reps) == 0 {
		p.log.Debug("skipped clipboard content without text")
		return model.ClipItem{}, false, nil
	}
	if size := inlineSize(reps); size > MaxInlineText {
		p.log.Info("skipped clipboard content above the inline limit", "size", size, "limit", MaxInlineText)
		return model.ClipItem{}, false, nil
	}

	item, err := p.newItem(ctx, reps)
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
func (p *Pipeline) newItem(ctx context.Context, reps []model.Representation) (model.ClipItem, error) {
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
		ExpiresAt:       now.Add(p.ttl),
		Kind:            model.KindText,
		Representations: reps,
		Source:          model.SourceClipboard,
	}, nil
}

// Apply handles an item announced by a peer. The item reaches the local
// clipboard only when the policy applies it.
func (p *Pipeline) Apply(ctx context.Context, from model.DeviceID, item model.ClipItem) error {
	if from.IsZero() {
		return errors.New("clip: peer device is required")
	}

	paused, err := p.pausedAt(ctx)
	if err != nil {
		return err
	}
	if paused {
		p.log.Debug("dropped incoming item while paused", "id", item.ID.String(), "origin", item.Origin.String())
		return nil
	}

	content, err := itemContent(item)
	if err != nil {
		return err
	}

	decision, err := p.policy.Decide(ctx, from, item)
	if err != nil {
		return fmt.Errorf("clip: decide policy: %w", err)
	}
	if decision != DecisionApply {
		p.log.Debug("incoming item not applied", "id", item.ID.String(), "decision", decision.String())
		return nil
	}

	// The hash is registered before the write so a watcher that reports the
	// change from inside Write already sees it as own content.
	p.echo.register(content)
	if err := p.clipboard.Write(ctx, content); err != nil {
		return fmt.Errorf("clip: write clipboard: %w", err)
	}
	if err := p.history.Append(ctx, item); err != nil {
		return fmt.Errorf("clip: record item: %w", err)
	}
	return nil
}

// Last returns the most recent item recorded in the history.
func (p *Pipeline) Last() (model.ClipItem, bool) {
	return p.history.Last()
}
