package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"lukechampine.com/blake3/bao"
)

// Put stores content of size bytes read at offsets of src. The content is
// streamed: the hashing pass and the encryption pass read it chunk by chunk.
// The key of the blob is the BLAKE3 hash of the whole content.
func (s *Store) Put(ctx context.Context, src io.ReaderAt, size int64) (Info, error) {
	if src == nil {
		return Info{}, errors.New("blob: source is required")
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
	if _, err := s.makeRoom(ctx, size); err != nil {
		return Info{}, err
	}

	lay := newLayout(size)
	f, name, err := s.createTemp()
	if err != nil {
		return Info{}, err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(name)
	}()

	if err := f.Truncate(lay.fileSize()); err != nil {
		return Info{}, fmt.Errorf("blob: size temporary file: %w", err)
	}

	// The hashing pass fills the tree of the file and yields the blob key.
	tree := sectionWriter{w: f, base: lay.treeOffset()}
	root, err := bao.Encode(tree, io.NewSectionReader(src, 0, size), size, baoGroup, true)
	if err != nil {
		return Info{}, fmt.Errorf("blob: hash content: %w", err)
	}
	if err := s.sealChunks(f, lay, src, root); err != nil {
		return Info{}, err
	}

	hdr := header{size: size, chunks: lay.chunks, root: root}
	if _, err := f.WriteAt(hdr.encode(), 0); err != nil {
		return Info{}, fmt.Errorf("blob: write header: %w", err)
	}
	if err := f.Sync(); err != nil {
		return Info{}, fmt.Errorf("blob: flush blob file: %w", err)
	}
	if err := f.Close(); err != nil {
		return Info{}, fmt.Errorf("blob: close blob file: %w", err)
	}

	existing, err := s.record(ctx, root)
	switch {
	case err == nil && !existing.info.Partial:
		// The content is already stored under the same key.
		if err := s.touch(ctx, root); err != nil {
			return Info{}, err
		}
		return existing.info, nil
	case err != nil && !errors.Is(err, ErrNotFound):
		return Info{}, err
	}

	info := Info{Hash: root, Size: size, Chunks: lay.chunks}
	if err := os.Rename(name, s.path(root)); err != nil {
		return Info{}, fmt.Errorf("blob: store %s: %w", hashHex(root), err)
	}
	if err := s.replaceRecord(ctx, info); err != nil {
		return Info{}, err
	}
	s.log.Info("stored blob", "hash", hashHex(root), "size", size, "chunks", lay.chunks)
	return info, nil
}

// sealChunks encrypts the content chunk by chunk into the file.
func (s *Store) sealChunks(f *os.File, lay layout, src io.ReaderAt, root [32]byte) error {
	plain := make([]byte, ChunkSize)
	for i := range lay.chunks {
		n := lay.chunkLen(i)
		chunk := plain[:n]
		if _, err := src.ReadAt(chunk, int64(i)*ChunkSize); err != nil {
			return fmt.Errorf("blob: read source chunk %d: %w", i, err)
		}
		sealed, err := s.sealer.Seal(chunkTable, chunkRowKey(root, i), chunk)
		if err != nil {
			return fmt.Errorf("blob: seal chunk %d: %w", i, err)
		}
		if _, err := f.WriteAt(sealed, lay.chunkOffset(i)); err != nil {
			return fmt.Errorf("blob: write chunk %d: %w", i, err)
		}
	}
	return nil
}

// createTemp creates a file that becomes a blob once it is complete.
func (s *Store) createTemp() (*os.File, string, error) {
	f, err := os.CreateTemp(s.dir, tempPrefix)
	if err != nil {
		return nil, "", fmt.Errorf("blob: create temporary file: %w", err)
	}
	return f, f.Name(), nil
}
