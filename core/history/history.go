package history

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/store"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Table names of the domain.
const (
	itemsTable = "history_items"
	blobsTable = "history_blobs"
)

// Lifetime defaults and limits.
const (
	// DefaultTextTTL is the lifetime of a text item.
	DefaultTextTTL = 7 * 24 * time.Hour
	// DefaultImageTTL is the lifetime of an image item.
	DefaultImageTTL = 3 * 24 * time.Hour
	// DefaultFilesTTL is the lifetime of a file item.
	DefaultFilesTTL = 24 * time.Hour
	// DefaultCleanupInterval is how often expired entries are removed.
	DefaultCleanupInterval = 10 * time.Minute
	// MaxTTL is the longest lifetime a device may set for a kind.
	MaxTTL = 365 * 24 * time.Hour
	// DefaultListLimit is the number of items a query returns when it asks for
	// no particular number.
	DefaultListLimit = 50
	// MaxListLimit is the largest number of items a query may return.
	MaxListLimit = 200
	// maxInlineBytes bounds the inline payload of one item.
	maxInlineBytes = 64 << 10
	// maxRepresentations bounds the representations of one item.
	maxRepresentations = 16
	// maxPayloadOpenBytes bounds the payload Copy reads into memory.
	maxPayloadOpenBytes = 64 << 20
)

// Errors reported by the history.
var (
	// ErrInvalidItem reports an item that cannot be recorded.
	ErrInvalidItem = errors.New("history: invalid item")
	// ErrNotFound reports an item that is absent, expired or deleted.
	ErrNotFound = errors.New("history: item not found")
	// ErrUnsupported reports an item the caller cannot be given.
	ErrUnsupported = errors.New("history: unsupported item")
	// ErrUnavailable reports a payload the device cannot produce.
	ErrUnavailable = errors.New("history: payload is not available")
	// ErrNoSource reports a payload no peer offers.
	ErrNoSource = errors.New("history: no peer has the payload")
)

// Settings reads the settings of the device. *store.Settings implements it.
type Settings interface {
	GetDuration(ctx context.Context, key string, def time.Duration) (time.Duration, error)
}

// Setting keys of the history.
const (
	SettingTextTTL  = "history.ttl.text"
	SettingImageTTL = "history.ttl.image"
	SettingFilesTTL = "history.ttl.files"
)

// TTLs are the lifetimes of captured items, one per kind.
type TTLs struct {
	Text  time.Duration
	Image time.Duration
	Files time.Duration
}

// DefaultTTLs returns the lifetimes of a device without history settings.
func DefaultTTLs() TTLs {
	return TTLs{Text: DefaultTextTTL, Image: DefaultImageTTL, Files: DefaultFilesTTL}
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

// LoadTTLs reads the lifetimes from the settings and falls back to the defaults
// for the keys that are absent or unusable.
func LoadTTLs(ctx context.Context, settings Settings) (TTLs, error) {
	ttls := DefaultTTLs()

	text, textErr := settings.GetDuration(ctx, SettingTextTTL, ttls.Text)
	image, imageErr := settings.GetDuration(ctx, SettingImageTTL, ttls.Image)
	files, filesErr := settings.GetDuration(ctx, SettingFilesTTL, ttls.Files)

	if text > 0 {
		ttls.Text = text
	}
	if image > 0 {
		ttls.Image = image
	}
	if files > 0 {
		ttls.Files = files
	}
	return ttls, errors.Join(textErr, imageErr, filesErr)
}

// Payload is the reader of one stored payload.
type Payload interface {
	// Size is the plaintext size in bytes.
	Size() int64
	// ReadAt reads the payload, verifying every chunk it touches.
	ReadAt(p []byte, off int64) (int, error)
	// Close releases the payload.
	Close() error
}

// Payloads is the local payload store. *blob.Store provides it through an
// adapter.
type Payloads interface {
	// Has reports whether the payload is stored on this device.
	Has(ctx context.Context, hash [32]byte) (bool, error)
	// Open returns a reader of a stored payload.
	Open(ctx context.Context, hash [32]byte) (Payload, error)
}

// Options configures a Store.
type Options struct {
	// Store is the database of the device. It is required.
	Store *store.Store
	// Origin is this device, recorded in the entries it creates. It is
	// required.
	Origin model.DeviceID
	// Payloads reads the local payload store. Without it the cached state of
	// every payload is used.
	Payloads Payloads
	// Clipboard receives the items Copy writes. Without it Copy reports
	// ErrUnavailable.
	Clipboard ports.Clipboard
	// Fetch downloads one payload from the peers. Without it Fetch cannot
	// restore a missing payload.
	Fetch func(ctx context.Context, hash [32]byte, size uint64) error
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// Logger receives history events. Nothing is logged when nil.
	Logger *slog.Logger
	// CleanupInterval is how often expired entries are removed.
	// DefaultCleanupInterval is used when zero.
	CleanupInterval time.Duration
	// OnChange is called after the store changed its entries, whoever wrote
	// them: it is how a reader learns that a new query is worth its cost.
	OnChange func()
}

// Store is the history log of one device.
type Store struct {
	db       *store.Store
	origin   model.DeviceID
	payloads Payloads
	clip     ports.Clipboard
	fetch    func(ctx context.Context, hash [32]byte, size uint64) error
	clock    ports.Clock
	log      *slog.Logger
	interval time.Duration
	onChange func()

	mu      sync.Mutex
	budgets map[model.DeviceID]int
	sending map[model.DeviceID]bool
}

// New creates the history of a device. The schema has to be migrated already.
func New(opts Options) (*Store, error) {
	if opts.Store == nil {
		return nil, errors.New("history: store is required")
	}
	if opts.Origin.IsZero() {
		return nil, errors.New("history: origin device is required")
	}

	s := &Store{
		db:       opts.Store,
		origin:   opts.Origin,
		payloads: opts.Payloads,
		clip:     opts.Clipboard,
		fetch:    opts.Fetch,
		clock:    opts.Clock,
		log:      opts.Logger,
		interval: opts.CleanupInterval,
		onChange: opts.OnChange,
		budgets:  make(map[model.DeviceID]int),
		sending:  make(map[model.DeviceID]bool),
	}
	if s.clock == nil {
		s.clock = ports.SystemClock
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.interval <= 0 {
		s.interval = DefaultCleanupInterval
	}
	return s, nil
}

// Origin returns the device this history belongs to.
func (s *Store) Origin() model.DeviceID {
	return s.origin
}

// Append records one item. An entry that is already known is kept, so items
// that arrive twice are recorded once.
func (s *Store) Append(ctx context.Context, item model.ClipItem) error {
	inserted, err := s.record(ctx, item)
	if err != nil {
		return err
	}
	if inserted {
		s.changed()
	}
	return nil
}

// record writes one entry and reports whether it was new.
func (s *Store) record(ctx context.Context, item model.ClipItem) (bool, error) {
	if err := validate(item); err != nil {
		return false, err
	}

	payload, err := s.sealPayload(item)
	if err != nil {
		return false, err
	}
	present, err := s.payloadState(ctx, item)
	if err != nil {
		return false, err
	}

	var inserted bool
	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		if inserted, err = insertItem(ctx, tx, item, payload); err != nil || !inserted {
			return err
		}
		if err := addReferences(ctx, tx, item, present); err != nil {
			return err
		}
		if item.Origin == s.origin {
			if err := advanceSequence(ctx, tx, item.Origin, item.Seq); err != nil {
				return err
			}
		}
		return recordVector(ctx, tx, item.Origin, item.Seq)
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

// changed reports a change of the entries to OnChange.
func (s *Store) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

// payloadState reports which payloads of the item are stored on this device.
func (s *Store) payloadState(ctx context.Context, item model.ClipItem) (map[[32]byte]bool, error) {
	state := make(map[[32]byte]bool)
	for _, rep := range item.Representations {
		if len(rep.Inline) > 0 || rep.Hash == [32]byte{} {
			continue
		}
		present, err := s.present(ctx, rep.Hash)
		if err != nil {
			return nil, err
		}
		state[rep.Hash] = present
	}
	return state, nil
}

// Last returns the most recent item that is neither expired nor deleted.
func (s *Store) Last() (model.ClipItem, bool) {
	items, err := s.List(context.Background(), ListFilter{Limit: 1})
	if err != nil {
		s.log.Warn("reading the latest history item failed", "error", err)
		return model.ClipItem{}, false
	}
	if len(items) == 0 {
		return model.ClipItem{}, false
	}
	return items[0].ClipItem, true
}

// validate checks the parts of an item the history relies on.
func validate(item model.ClipItem) error {
	switch {
	case item.ID.IsZero():
		return fmt.Errorf("%w: id is zero", ErrInvalidItem)
	case item.Origin.IsZero():
		return fmt.Errorf("%w: origin is zero", ErrInvalidItem)
	case item.Seq == 0:
		return fmt.Errorf("%w: sequence is zero", ErrInvalidItem)
	case item.CreatedAt.IsZero():
		return fmt.Errorf("%w: created time is zero", ErrInvalidItem)
	case item.ExpiresAt.IsZero() || !item.ExpiresAt.After(item.CreatedAt):
		return fmt.Errorf("%w: expiry is not after the creation time", ErrInvalidItem)
	case len(item.Representations) > maxRepresentations:
		return fmt.Errorf("%w: %d representations, limit %d", ErrInvalidItem, len(item.Representations), maxRepresentations)
	}
	if item.Deleted {
		return nil
	}

	var inline int
	for i, rep := range item.Representations {
		if rep.MIME == "" {
			return fmt.Errorf("%w: representation %d has no mime type", ErrInvalidItem, i)
		}
		if rep.Hash == [32]byte{} {
			return fmt.Errorf("%w: representation %d has no hash", ErrInvalidItem, i)
		}
		inline += len(rep.Inline)
	}
	if inline > maxInlineBytes {
		return fmt.Errorf("%w: inline payload is %d bytes, limit %d", ErrInvalidItem, inline, maxInlineBytes)
	}
	return nil
}

// sealPayload seals the representations of item for the row key.
func (s *Store) sealPayload(item model.ClipItem) ([]byte, error) {
	reps := make([]*wirev1.Representation, 0, len(item.Representations))
	for _, rep := range item.Representations {
		reps = append(reps, protoconv.RepresentationToProto(rep))
	}
	data, err := proto.Marshal(&wirev1.ClipItem{Representations: reps})
	if err != nil {
		return nil, fmt.Errorf("history: encode item: %w", err)
	}
	sealed, err := s.db.Sealer().Seal(itemsTable, rowKey(item.Origin, item.Seq), data)
	if err != nil {
		return nil, fmt.Errorf("history: seal item: %w", err)
	}
	return sealed, nil
}

// openPayload opens the representations of one entry.
func (s *Store) openPayload(origin model.DeviceID, seq uint64, sealed []byte) ([]model.Representation, error) {
	data, err := s.db.Sealer().Open(itemsTable, rowKey(origin, seq), sealed)
	if err != nil {
		return nil, err
	}
	msg := &wirev1.ClipItem{}
	if err := proto.Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("history: decode item: %w", err)
	}
	reps := make([]model.Representation, 0, len(msg.GetRepresentations()))
	for i, rep := range msg.GetRepresentations() {
		converted, err := protoconv.RepresentationFromProto(rep)
		if err != nil {
			return nil, fmt.Errorf("history: representation %d: %w", i, err)
		}
		reps = append(reps, converted)
	}
	return reps, nil
}

// rowKey identifies one log entry to the sealer.
func rowKey(origin model.DeviceID, seq uint64) string {
	return hex.EncodeToString(origin[:]) + "/" + strconv.FormatUint(seq, 10)
}

// insertItem writes one entry and reports whether it is new.
func insertItem(ctx context.Context, tx *sql.Tx, item model.ClipItem, payload []byte) (bool, error) {
	const query = `
		INSERT INTO history_items (origin, seq, id, created_at, expires_at, kind, source, deleted, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (origin, seq) DO NOTHING`

	result, err := tx.ExecContext(ctx, query,
		item.Origin[:],
		int64(item.Seq),
		item.ID[:],
		item.CreatedAt.UnixMilli(),
		item.ExpiresAt.UnixMilli(),
		int64(item.Kind),
		int64(item.Source),
		boolInt(item.Deleted),
		payload,
	)
	if err != nil {
		return false, fmt.Errorf("history: write item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("history: write item: %w", err)
	}
	return rows == 1, nil
}

// addReferences records the payloads the item points to.
func addReferences(ctx context.Context, tx *sql.Tx, item model.ClipItem, present map[[32]byte]bool) error {
	const query = `
		INSERT INTO history_blobs (hash, size, refs, present) VALUES (?, ?, 1, ?)
		ON CONFLICT (hash) DO UPDATE SET
			refs = refs + 1,
			size = MAX(size, excluded.size),
			present = MAX(present, excluded.present)`

	for _, rep := range item.Representations {
		if len(rep.Inline) > 0 || rep.Hash == [32]byte{} {
			continue
		}
		if _, err := tx.ExecContext(ctx, query, rep.Hash[:], int64(rep.Size), boolInt(present[rep.Hash])); err != nil {
			return fmt.Errorf("history: reference payload: %w", err)
		}
	}
	return nil
}

// releaseReferences drops the payload references of the given entries.
func releaseReferences(ctx context.Context, tx *sql.Tx, hashes [][32]byte) error {
	const query = `UPDATE history_blobs SET refs = refs - 1 WHERE hash = ?`
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx, query, hash[:]); err != nil {
			return fmt.Errorf("history: release payload: %w", err)
		}
	}
	return nil
}

// recordVector moves the version of origin forward.
func recordVector(ctx context.Context, tx *sql.Tx, origin model.DeviceID, seq uint64) error {
	const query = `
		INSERT INTO history_vectors (origin, seq) VALUES (?, ?)
		ON CONFLICT (origin) DO UPDATE SET seq = MAX(seq, excluded.seq)`
	if _, err := tx.ExecContext(ctx, query, origin[:], int64(seq)); err != nil {
		return fmt.Errorf("history: record version: %w", err)
	}
	return nil
}

// advanceSequence keeps the sequence counter of this device above a recorded
// entry, whoever created it.
func advanceSequence(ctx context.Context, tx *sql.Tx, origin model.DeviceID, seq uint64) error {
	const query = `
		INSERT INTO clip_seq (origin, seq) VALUES (?, ?)
		ON CONFLICT (origin) DO UPDATE SET seq = MAX(seq, excluded.seq)`
	if _, err := tx.ExecContext(ctx, query, origin[:], int64(seq)); err != nil {
		return fmt.Errorf("history: advance sequence: %w", err)
	}
	return nil
}

// nextSeq allocates the next sequence number of this device.
func (s *Store) nextSeq(ctx context.Context) (uint64, error) {
	const query = `
		INSERT INTO clip_seq (origin, seq) VALUES (?, 1)
		ON CONFLICT (origin) DO UPDATE SET seq = seq + 1
		RETURNING seq`

	var seq int64
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, query, s.origin[:]).Scan(&seq)
	})
	if err != nil {
		return 0, fmt.Errorf("history: allocate sequence: %w", err)
	}
	if seq <= 0 {
		return 0, errors.New("history: allocated an invalid sequence")
	}
	return uint64(seq), nil
}

// present reports whether the payload is stored on this device.
func (s *Store) present(ctx context.Context, hash [32]byte) (bool, error) {
	if s.payloads != nil {
		return s.payloads.Has(ctx, hash)
	}

	var present int
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT present FROM history_blobs WHERE hash = ?`, hash[:]).Scan(&present)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("history: read payload state: %w", err)
	}
	return present == 1, nil
}

// boolInt converts a boolean to its stored form.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
