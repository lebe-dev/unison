package blob

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/store"
)

// ChunkSize is the plaintext size of a stored chunk. It is also the group size
// of the hash tree, so a chunk is verified with a single tree path.
const ChunkSize = 1 << 20

// Size limits used when nothing else is configured.
const (
	// DefaultLimitDesktop is the store limit of a desktop node in bytes.
	DefaultLimitDesktop = 2 << 30
	// DefaultLimitMobile is the store limit of a mobile node in bytes.
	DefaultLimitMobile = 500 << 20
)

// SettingMaxSize is the setting holding the size limit in bytes.
const SettingMaxSize = "blob.max_size"

// Errors reported by the store.
var (
	// ErrNotFound reports a blob that the store does not know.
	ErrNotFound = errors.New("blob: blob not found")
	// ErrPartial reports a blob that has not received all chunks yet.
	ErrPartial = errors.New("blob: blob is incomplete")
	// ErrComplete reports a blob that already holds all its chunks.
	ErrComplete = errors.New("blob: blob is complete")
	// ErrChunkRange reports a chunk index outside the blob.
	ErrChunkRange = errors.New("blob: chunk index is out of range")
	// ErrChunkMismatch reports a chunk that does not hash to the tree of the
	// blob.
	ErrChunkMismatch = errors.New("blob: chunk does not match the blob hash")
	// ErrCorrupt reports a stored chunk that cannot be decrypted.
	ErrCorrupt = errors.New("blob: stored chunk cannot be opened")
	// ErrSize reports a negative content size.
	ErrSize = errors.New("blob: content size is invalid")
	// ErrTooLarge reports content above the size limit of the store.
	ErrTooLarge = errors.New("blob: content exceeds the size limit")
	// ErrNoSpace reports that the limit cannot be met by evicting blobs.
	ErrNoSpace = errors.New("blob: no space left in the store")
	// ErrMissingChunks reports a blob completed while chunks are absent.
	ErrMissingChunks = errors.New("blob: blob has missing chunks")
	// ErrBadFormat reports a blob file this code cannot read.
	ErrBadFormat = errors.New("blob: unsupported blob file")
)

// Info describes a stored blob.
type Info struct {
	// Hash is the BLAKE3 hash of the whole content.
	Hash [32]byte
	// Size is the plaintext size in bytes.
	Size int64
	// Chunks is the number of chunks the content is split into.
	Chunks int
	// Partial reports a blob that is still receiving chunks.
	Partial bool
}

// Options configures New.
type Options struct {
	// Dir is the directory of the blob files. It is created when missing.
	Dir string
	// Store keeps the blob metadata and seals the chunks.
	Store *store.Store
	// Limit is the size limit in bytes used when the setting is absent.
	// DefaultLimitDesktop is used when zero.
	Limit int64
	// Clock is the time source of the metadata. The system clock is used when
	// nil.
	Clock ports.Clock
	// Logger receives store events. Nothing is logged when nil.
	Logger *slog.Logger
}

// Store keeps blobs in a directory and their metadata in the node database.
type Store struct {
	dir    string
	db     *store.Store
	sealer *store.Sealer
	limit  int64
	clock  ports.Clock
	log    *slog.Logger

	mu   sync.Mutex
	pins map[[32]byte]int
}

// New opens the blob store in opts.Dir.
func New(opts Options) (*Store, error) {
	if opts.Dir == "" {
		return nil, errors.New("blob: directory is required")
	}
	if opts.Store == nil {
		return nil, errors.New("blob: store is required")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("blob: create directory: %w", err)
	}

	s := &Store{
		dir:    opts.Dir,
		db:     opts.Store,
		sealer: opts.Store.Sealer(),
		limit:  opts.Limit,
		clock:  opts.Clock,
		log:    opts.Logger,
		pins:   make(map[[32]byte]int),
	}
	if s.limit <= 0 {
		s.limit = DefaultLimitDesktop
	}
	if s.clock == nil {
		s.clock = ports.SystemClock
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}

	// Files left behind by an interrupted write are unusable.
	s.removeTempFiles()
	return s, nil
}

// Dir returns the directory of the blob files.
func (s *Store) Dir() string {
	return s.dir
}

// Limit returns the size limit in bytes, read from the setting when present.
func (s *Store) Limit(ctx context.Context) (int64, error) {
	limit, err := s.db.Settings().GetInt(ctx, SettingMaxSize, s.limit)
	if err != nil {
		return 0, fmt.Errorf("blob: read size limit: %w", err)
	}
	if limit <= 0 {
		return s.limit, nil
	}
	return limit, nil
}

// Has reports whether the blob is stored with all its chunks.
func (s *Store) Has(ctx context.Context, hash [32]byte) (bool, error) {
	rec, err := s.record(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if rec.info.Partial {
		return false, nil
	}
	if _, err := os.Stat(s.path(hash)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("blob: stat %s: %w", hashHex(hash), err)
	}
	return true, nil
}

// Stat returns the metadata of a blob, complete or partial.
func (s *Store) Stat(ctx context.Context, hash [32]byte) (Info, error) {
	rec, err := s.record(ctx, hash)
	if err != nil {
		return Info{}, err
	}
	return rec.info, nil
}

// Remove deletes a blob and its metadata, including one pinned by a caller.
func (s *Store) Remove(ctx context.Context, hash [32]byte) error {
	if _, err := s.record(ctx, hash); err != nil {
		return err
	}
	return s.remove(ctx, hash)
}

// Tree returns the hash tree of a blob. It is the proof a peer needs to verify
// every chunk of the content against the hash.
func (s *Store) Tree(ctx context.Context, hash [32]byte) ([]byte, error) {
	rec, err := s.record(ctx, hash)
	if err != nil {
		return nil, err
	}
	return s.readTree(hash, newLayout(rec.info.Size))
}

// path returns the file of a blob.
func (s *Store) path(hash [32]byte) string {
	return filepath.Join(s.dir, hashHex(hash))
}

// now returns the current time of the configured clock.
func (s *Store) now() time.Time {
	return s.clock.Now()
}

// removeTempFiles deletes the leftovers of interrupted writes.
func (s *Store) removeTempFiles() {
	names, err := filepath.Glob(filepath.Join(s.dir, tempPrefix+"*"))
	if err != nil {
		s.log.Warn("listing temporary files failed", "error", err)
		return
	}
	for _, name := range names {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("removing temporary file failed", "error", err)
		}
	}
}

// hashHex returns the hexadecimal form of a hash used in file names and logs.
func hashHex(hash [32]byte) string {
	return hex.EncodeToString(hash[:])
}

// isZero reports a hash that cannot address any content.
func isZero(hash [32]byte) bool {
	return hash == [32]byte{}
}
