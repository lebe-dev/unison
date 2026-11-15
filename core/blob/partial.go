package blob

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// CreatePartial starts receiving a blob of size bytes with the given hash. tree
// is the hash tree of the content, which the store needs to verify every chunk
// as it arrives. Calling it again for a blob that is still incomplete keeps the
// chunks already stored.
func (s *Store) CreatePartial(ctx context.Context, hash [32]byte, size int64, tree []byte) (Info, error) {
	if isZero(hash) {
		return Info{}, fmt.Errorf("%w: zero hash", ErrBadFormat)
	}
	if size < 0 {
		return Info{}, fmt.Errorf("%w: %d bytes", ErrSize, size)
	}
	limit, err := s.Limit(ctx)
	if err != nil {
		return Info{}, err
	}
	if size > limit {
		return Info{}, fmt.Errorf("%w: %d bytes, limit is %d bytes", ErrTooLarge, size, limit)
	}

	lay := newLayout(size)
	if int64(len(tree)) != lay.treeSize() {
		return Info{}, fmt.Errorf("%w: tree is %d bytes, want %d", ErrBadFormat, len(tree), lay.treeSize())
	}
	if err := checkTree(tree, size); err != nil {
		return Info{}, err
	}

	rec, err := s.record(ctx, hash)
	switch {
	case err == nil && !rec.info.Partial:
		return Info{}, fmt.Errorf("%w: %s", ErrComplete, hashHex(hash))
	case err == nil:
		stored, err := s.readTree(hash, lay)
		if err != nil {
			return Info{}, err
		}
		if !bytes.Equal(stored, tree) {
			return Info{}, fmt.Errorf("%w: tree of %s differs from the stored one", ErrBadFormat, hashHex(hash))
		}
		return rec.info, nil
	case !errors.Is(err, ErrNotFound):
		return Info{}, err
	}

	if _, err := s.makeRoom(ctx, size); err != nil {
		return Info{}, err
	}

	f, name, err := s.createTemp()
	if err != nil {
		return Info{}, err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(name)
	}()

	hdr := header{size: size, chunks: lay.chunks, root: hash}
	if _, err := f.WriteAt(hdr.encode(), 0); err != nil {
		return Info{}, fmt.Errorf("blob: write header: %w", err)
	}
	if _, err := f.WriteAt(tree, lay.treeOffset()); err != nil {
		return Info{}, fmt.Errorf("blob: write tree: %w", err)
	}
	if err := f.Truncate(lay.fileSize()); err != nil {
		return Info{}, fmt.Errorf("blob: size blob file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return Info{}, fmt.Errorf("blob: flush blob file: %w", err)
	}
	if err := f.Close(); err != nil {
		return Info{}, fmt.Errorf("blob: close blob file: %w", err)
	}
	if err := os.Rename(name, s.path(hash)); err != nil {
		return Info{}, fmt.Errorf("blob: start %s: %w", hashHex(hash), err)
	}

	info := Info{Hash: hash, Size: size, Chunks: lay.chunks, Partial: true}
	if err := s.insertPartial(ctx, info); err != nil {
		return Info{}, err
	}
	s.log.Info("receiving blob", "hash", hashHex(hash), "size", size, "chunks", lay.chunks)
	return info, nil
}

// WriteChunk stores the plaintext chunk at index of a partial blob. The chunk
// is verified against the hash tree before it is sealed and written, and it is
// recorded only once it reached the disk.
func (s *Store) WriteChunk(ctx context.Context, hash [32]byte, index int, data []byte) error {
	rec, err := s.record(ctx, hash)
	if err != nil {
		return err
	}
	if !rec.info.Partial {
		return fmt.Errorf("%w: %s", ErrComplete, hashHex(hash))
	}

	f, err := os.OpenFile(s.path(hash), os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("blob: open %s: %w", hashHex(hash), err)
	}
	defer func() { _ = f.Close() }()

	lay := newLayout(rec.info.Size)
	chunks := openChunkFile(f, lay, hash)
	if err := chunks.verify(index, data); err != nil {
		return err
	}

	sealed, err := s.sealer.Seal(chunkTable, chunkRowKey(hash, index), data)
	if err != nil {
		return fmt.Errorf("blob: seal chunk %d: %w", index, err)
	}
	if _, err := f.WriteAt(sealed, lay.chunkOffset(index)); err != nil {
		return fmt.Errorf("blob: write chunk %d: %w", index, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("blob: flush chunk %d: %w", index, err)
	}
	return s.markChunk(ctx, hash, index, lay.chunks)
}

// Missing returns the indices of the chunks a partial blob still needs. It is
// empty for a complete blob.
func (s *Store) Missing(ctx context.Context, hash [32]byte) ([]int, error) {
	rec, err := s.record(ctx, hash)
	if err != nil {
		return nil, err
	}
	if !rec.info.Partial {
		return nil, nil
	}
	return missingChunks(rec.received, rec.info.Chunks), nil
}

// Complete turns a partial blob into a stored one. It reports the chunks that
// are still absent and does nothing for a blob that is already complete.
func (s *Store) Complete(ctx context.Context, hash [32]byte) (Info, error) {
	rec, err := s.record(ctx, hash)
	if err != nil {
		return Info{}, err
	}
	if !rec.info.Partial {
		return rec.info, nil
	}

	if missing := missingChunks(rec.received, rec.info.Chunks); len(missing) > 0 {
		return Info{}, fmt.Errorf("%w: %d of %d chunks", ErrMissingChunks, len(missing), rec.info.Chunks)
	}
	if err := s.markComplete(ctx, hash); err != nil {
		return Info{}, err
	}

	info := rec.info
	info.Partial = false
	s.log.Info("blob complete", "hash", hashHex(hash), "size", info.Size, "chunks", info.Chunks)
	return info, nil
}

// checkTree validates the length prefix of a hash tree.
func checkTree(tree []byte, size int64) error {
	if binary.LittleEndian.Uint64(tree[:treeHeaderSize]) != uint64(size) {
		return fmt.Errorf("%w: tree describes %d bytes, want %d", ErrBadFormat, binary.LittleEndian.Uint64(tree), size)
	}
	return nil
}

// readTree reads the hash tree of a stored blob.
func (s *Store) readTree(hash [32]byte, lay layout) ([]byte, error) {
	tree := make([]byte, lay.treeSize())
	f, err := os.Open(s.path(hash))
	if err != nil {
		return nil, fmt.Errorf("blob: open %s: %w", hashHex(hash), err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.ReadAt(tree, lay.treeOffset()); err != nil {
		return nil, fmt.Errorf("blob: read tree of %s: %w", hashHex(hash), err)
	}
	return tree, nil
}
