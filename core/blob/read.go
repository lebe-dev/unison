package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/lebe-dev/unison/core/store"
)

// Reader reads the plaintext of a stored blob. Every chunk is decrypted and
// verified against the hash tree before any of its bytes are returned.
type Reader struct {
	chunks *chunkFile
	sealer *store.Sealer
	info   Info

	last   int
	cached []byte
}

// Open opens a complete blob for reading and marks it as recently used.
func (s *Store) Open(ctx context.Context, hash [32]byte) (*Reader, error) {
	rec, err := s.record(ctx, hash)
	if err != nil {
		return nil, err
	}
	if rec.info.Partial {
		missing := len(missingChunks(rec.received, rec.info.Chunks))
		return nil, fmt.Errorf("%w: %s, %d of %d chunks missing", ErrPartial, hashHex(hash), missing, rec.info.Chunks)
	}

	lay := newLayout(rec.info.Size)
	f, err := os.Open(s.path(hash))
	if err != nil {
		return nil, fmt.Errorf("blob: open %s: %w", hashHex(hash), err)
	}
	hdr, err := readHeader(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", hashHex(hash), err)
	}
	if hdr.size != rec.info.Size || hdr.chunks != rec.info.Chunks || hdr.root != hash {
		_ = f.Close()
		return nil, fmt.Errorf("%w: header of %s does not match its metadata", ErrBadFormat, hashHex(hash))
	}
	if err := s.touch(ctx, hash); err != nil {
		_ = f.Close()
		return nil, err
	}

	info := rec.info
	info.Partial = false
	return &Reader{
		chunks: openChunkFile(f, lay, hash),
		sealer: s.sealer,
		info:   info,
		last:   -1,
	}, nil
}

// Info returns the metadata of the open blob.
func (r *Reader) Info() Info {
	return r.info
}

// Size returns the plaintext size of the blob in bytes.
func (r *Reader) Size() int64 {
	return r.info.Size
}

// ReadAt reads len(p) bytes at off, verifying every chunk it touches. It
// implements io.ReaderAt.
func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("blob: negative offset")
	}
	if len(p) == 0 {
		return 0, nil
	}
	size := r.info.Size
	if off >= size {
		return 0, io.EOF
	}

	read := 0
	for read < len(p) {
		pos := off + int64(read)
		if pos >= size {
			break
		}
		chunk, err := r.chunk(int(pos / ChunkSize))
		if err != nil {
			return read, err
		}
		read += copy(p[read:], chunk[pos%ChunkSize:])
	}
	if read < len(p) {
		return read, io.EOF
	}
	return read, nil
}

// ReadChunk returns the plaintext of the chunk at index.
func (r *Reader) ReadChunk(index int) ([]byte, error) {
	data, err := r.chunk(index)
	if err != nil {
		return nil, err
	}
	return slices.Clone(data), nil
}

// Close releases the blob file.
func (r *Reader) Close() error {
	r.last, r.cached = -1, nil
	return r.chunks.file.Close()
}

// chunk returns the verified chunk at index, keeping the last one for reads
// that span it.
func (r *Reader) chunk(index int) ([]byte, error) {
	if index == r.last {
		return r.cached, nil
	}
	data, err := r.chunks.read(r.sealer, index)
	if err != nil {
		return nil, err
	}
	r.last, r.cached = index, data
	return data, nil
}

// ReadChunk returns the plaintext of the chunk at index of a complete blob.
func (s *Store) ReadChunk(ctx context.Context, hash [32]byte, index int) ([]byte, error) {
	r, err := s.Open(ctx, hash)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	return r.ReadChunk(index)
}

// readHeader parses the header of an open blob file.
func readHeader(f *os.File) (header, error) {
	raw := make([]byte, headerSize)
	if _, err := f.ReadAt(raw, 0); err != nil {
		return header{}, fmt.Errorf("%w: read header: %w", ErrBadFormat, err)
	}
	return decodeHeader(raw)
}
