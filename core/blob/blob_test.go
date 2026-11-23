package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"lukechampine.com/blake3"
)

func TestPutReadRoundTrip(t *testing.T) {
	s := newTestStore(t, DefaultLimitDesktop)
	sizes := []int{0, 1, 1000, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 12345}

	for _, size := range sizes {
		t.Run(sizeName(size), func(t *testing.T) {
			data := randomBytes(t, size)
			info := mustPut(t, s, data)
			if want := blake3.Sum256(data); info.Hash != want {
				t.Fatalf("hash is %s, want %s", hashHex(info.Hash), hashHex(want))
			}
			if info.Size != int64(size) {
				t.Fatalf("size is %d, want %d", info.Size, size)
			}
			if want := chunksFor(int64(size)); info.Chunks != want {
				t.Fatalf("chunks is %d, want %d", info.Chunks, want)
			}
			if info.Partial {
				t.Fatal("stored blob is partial")
			}

			if has, err := s.Has(context.Background(), info.Hash); err != nil || !has {
				t.Fatalf("has = %v, %v", has, err)
			}
			stat, err := s.Stat(context.Background(), info.Hash)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if stat != info {
				t.Fatalf("stat is %+v, want %+v", stat, info)
			}

			read := readAll(t, s, info.Hash)
			if !bytes.Equal(read, data) {
				t.Fatalf("content differs: %d bytes read, want %d", len(read), len(data))
			}
			checkChunks(t, s, info, data)
		})
	}
}

func TestPutEncryptsChunks(t *testing.T) {
	s := newTestStore(t, DefaultLimitDesktop)
	data := bytes.Repeat([]byte("sensitive payload "), ChunkSize/16)
	info := mustPut(t, s, data)

	raw, err := os.ReadFile(s.path(info.Hash))
	if err != nil {
		t.Fatalf("read blob file: %v", err)
	}
	if bytes.Contains(raw, data[:1024]) {
		t.Fatal("blob file holds plaintext")
	}
	if want := newLayout(int64(len(data))).fileSize(); int64(len(raw)) != want {
		t.Fatalf("file is %d bytes, want %d", len(raw), want)
	}
}

func TestReadAtSpansChunks(t *testing.T) {
	s := newTestStore(t, DefaultLimitDesktop)
	data := randomBytes(t, 2*ChunkSize+100)
	info := mustPut(t, s, data)

	r, err := s.Open(context.Background(), info.Hash)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = r.Close() }()

	cases := []struct {
		off  int64
		size int
	}{
		{0, 10},
		{int64(ChunkSize) - 5, 10},
		{int64(ChunkSize), 1},
		{int64(ChunkSize) - 4096, 8192},
		{int64(len(data)) - 1, 1},
		{int64(len(data)) - 10, 10},
	}
	for _, c := range cases {
		buf := make([]byte, c.size)
		n, err := r.ReadAt(buf, c.off)
		if err != nil {
			t.Fatalf("read %d bytes at %d: %v", c.size, c.off, err)
		}
		if n != c.size {
			t.Fatalf("read %d bytes at %d, want %d", n, c.off, c.size)
		}
		if !bytes.Equal(buf, data[c.off:c.off+int64(c.size)]) {
			t.Fatalf("content at %d differs", c.off)
		}
	}

	if n, err := r.ReadAt(make([]byte, 10), int64(len(data))); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read past the end = %d, %v", n, err)
	}
	if _, err := r.ReadChunk(info.Chunks); !errors.Is(err, ErrChunkRange) {
		t.Fatalf("read chunk out of range: %v", err)
	}
}

func TestCorruptedChunkIsDetected(t *testing.T) {
	data := randomBytes(t, 2*ChunkSize+10)
	lay := newLayout(int64(len(data)))

	t.Run("ciphertext", func(t *testing.T) {
		s := newTestStore(t, DefaultLimitDesktop)
		info := mustPut(t, s, data)
		file := readFile(t, s, info.Hash)
		file[lay.chunkOffset(0)+chunkOverhead+100] ^= 0xff
		if err := os.WriteFile(s.path(info.Hash), file, 0o600); err != nil {
			t.Fatalf("write blob file: %v", err)
		}
		if _, err := s.ReadChunk(context.Background(), info.Hash, 0); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("read chunk: %v, want ErrCorrupt", err)
		}
	})

	t.Run("tree", func(t *testing.T) {
		s := newTestStore(t, DefaultLimitDesktop)
		info := mustPut(t, s, data)
		file := readFile(t, s, info.Hash)
		// The first chaining value of the root pair covers the whole tree.
		file[lay.treeOffset()+treeHeaderSize] ^= 0xff
		if err := os.WriteFile(s.path(info.Hash), file, 0o600); err != nil {
			t.Fatalf("write blob file: %v", err)
		}
		if _, err := s.ReadChunk(context.Background(), info.Hash, 0); !errors.Is(err, ErrChunkMismatch) {
			t.Fatalf("read chunk: %v, want ErrChunkMismatch", err)
		}
	})

	t.Run("plaintext", func(t *testing.T) {
		s := newTestStore(t, DefaultLimitDesktop)
		info := mustPut(t, s, data)
		other := randomBytes(t, ChunkSize)
		sealed, err := s.sealer.Seal(chunkTable, chunkRowKey(info.Hash, 1), other)
		if err != nil {
			t.Fatalf("seal chunk: %v", err)
		}
		f, err := os.OpenFile(s.path(info.Hash), os.O_RDWR, 0)
		if err != nil {
			t.Fatalf("open blob file: %v", err)
		}
		defer func() { _ = f.Close() }()
		if _, err := f.WriteAt(sealed, lay.chunkOffset(1)); err != nil {
			t.Fatalf("write chunk: %v", err)
		}
		if _, err := s.ReadChunk(context.Background(), info.Hash, 1); !errors.Is(err, ErrChunkMismatch) {
			t.Fatalf("read chunk: %v, want ErrChunkMismatch", err)
		}
	})
}

func TestPutReusesStoredContent(t *testing.T) {
	s := newTestStore(t, DefaultLimitDesktop)
	data := randomBytes(t, ChunkSize+1)

	first := mustPut(t, s, data)
	second := mustPut(t, s, data)
	if first != second {
		t.Fatalf("second put is %+v, want %+v", second, first)
	}
	if usage, err := s.Usage(context.Background()); err != nil || usage != int64(len(data)) {
		t.Fatalf("usage = %d, %v", usage, err)
	}
	if files := blobFiles(t, s); len(files) != 1 {
		t.Fatalf("directory holds %d files, want 1", len(files))
	}
}

func TestPutRejectsContentAboveLimit(t *testing.T) {
	s := newTestStore(t, 2*ChunkSize)
	if _, err := s.Put(context.Background(), bytes.NewReader(make([]byte, 2*ChunkSize+1)), 2*ChunkSize+1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("put above the limit: %v, want ErrTooLarge", err)
	}
	if _, err := s.Put(context.Background(), bytes.NewReader(nil), -1); !errors.Is(err, ErrSize) {
		t.Fatalf("put negative size: %v, want ErrSize", err)
	}
}

func TestRemove(t *testing.T) {
	s := newTestStore(t, DefaultLimitDesktop)
	info := mustPut(t, s, randomBytes(t, 10))

	if err := s.Remove(context.Background(), info.Hash); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if has, err := s.Has(context.Background(), info.Hash); err != nil || has {
		t.Fatalf("has = %v, %v", has, err)
	}
	if _, err := s.Stat(context.Background(), info.Hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat after remove: %v, want ErrNotFound", err)
	}
	if files := blobFiles(t, s); len(files) != 0 {
		t.Fatalf("directory holds %d files, want 0", len(files))
	}
	if err := s.Remove(context.Background(), info.Hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second remove: %v, want ErrNotFound", err)
	}
}

// readAll returns the whole plaintext of a stored blob.
func readAll(t *testing.T, s *Store, hash [32]byte) []byte {
	t.Helper()
	r, err := s.Open(context.Background(), hash)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(io.NewSectionReader(r, 0, r.Size()))
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	return data
}

// checkChunks compares every chunk of a stored blob with data.
func checkChunks(t *testing.T, s *Store, info Info, data []byte) {
	t.Helper()
	lay := newLayout(int64(len(data)))
	for i := range info.Chunks {
		chunk, err := s.ReadChunk(context.Background(), info.Hash, i)
		if err != nil {
			t.Fatalf("read chunk %d: %v", i, err)
		}
		from := int64(i) * ChunkSize
		if want := data[from : from+int64(lay.chunkLen(i))]; !bytes.Equal(chunk, want) {
			t.Fatalf("chunk %d differs", i)
		}
	}
}

// readFile returns the bytes of a blob file.
func readFile(t *testing.T, s *Store, hash [32]byte) []byte {
	t.Helper()
	data, err := os.ReadFile(s.path(hash))
	if err != nil {
		t.Fatalf("read blob file: %v", err)
	}
	return data
}

// blobFiles returns the blob files of the store.
func blobFiles(t *testing.T, s *Store) []string {
	t.Helper()
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatalf("read blob directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

// sizeName returns a test name for a content size.
func sizeName(size int) string {
	return fmt.Sprintf("%d_bytes", size)
}
