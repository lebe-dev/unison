package blob

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/lebe-dev/unison/core/ports/fake"
)

func TestPartialWriteAndResume(t *testing.T) {
	ctx := context.Background()
	data := randomBytes(t, 3*ChunkSize+777)
	lay := newLayout(int64(len(data)))

	sender := newTestStore(t, DefaultLimitDesktop)
	info := mustPut(t, sender, data)
	tree, err := sender.Tree(ctx, info.Hash)
	if err != nil {
		t.Fatalf("read tree: %v", err)
	}

	chunk := func(i int) []byte {
		from := int64(i) * ChunkSize
		return data[from : from+int64(lay.chunkLen(i))]
	}

	dirs := newTestDirs(t)
	keystore := fake.NewKeystore()
	receiver := newTestStoreAt(t, dirs, keystore, DefaultLimitDesktop)

	started, err := receiver.blobs.CreatePartial(ctx, info.Hash, info.Size, tree)
	if err != nil {
		t.Fatalf("create partial: %v", err)
	}
	if !started.Partial {
		t.Fatal("started blob is complete")
	}
	if missing, err := receiver.blobs.Missing(ctx, info.Hash); err != nil || len(missing) != info.Chunks {
		t.Fatalf("missing = %v, %v, want %d chunks", missing, err, info.Chunks)
	}
	if has, err := receiver.blobs.Has(ctx, info.Hash); err != nil || has {
		t.Fatalf("has = %v, %v", has, err)
	}
	if _, err := receiver.blobs.Open(ctx, info.Hash); !errors.Is(err, ErrPartial) {
		t.Fatalf("open partial: %v, want ErrPartial", err)
	}

	// Chunks arrive out of order and one of them is corrupt.
	for _, index := range []int{0, 2, 3} {
		if err := receiver.blobs.WriteChunk(ctx, info.Hash, index, chunk(index)); err != nil {
			t.Fatalf("write chunk %d: %v", index, err)
		}
	}
	if err := receiver.blobs.WriteChunk(ctx, info.Hash, 1, chunk(0)); !errors.Is(err, ErrChunkMismatch) {
		t.Fatalf("write wrong chunk: %v, want ErrChunkMismatch", err)
	}
	if missing, err := receiver.blobs.Missing(ctx, info.Hash); err != nil || !slices.Equal(missing, []int{1}) {
		t.Fatalf("missing = %v, %v, want [1]", missing, err)
	}

	// A restart keeps the chunks that already arrived.
	if err := receiver.db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	restarted := newTestStoreAt(t, dirs, keystore, DefaultLimitDesktop).blobs

	if missing, err := restarted.Missing(ctx, info.Hash); err != nil || !slices.Equal(missing, []int{1}) {
		t.Fatalf("missing after restart = %v, %v, want [1]", missing, err)
	}
	if _, err := restarted.CreatePartial(ctx, info.Hash, info.Size, tree); err != nil {
		t.Fatalf("create partial for a known blob: %v", err)
	}
	if _, err := restarted.Complete(ctx, info.Hash); !errors.Is(err, ErrMissingChunks) {
		t.Fatalf("complete with a missing chunk: %v, want ErrMissingChunks", err)
	}

	if err := restarted.WriteChunk(ctx, info.Hash, 1, chunk(1)); err != nil {
		t.Fatalf("write chunk: %v", err)
	}
	completed, err := restarted.Complete(ctx, info.Hash)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Partial {
		t.Fatal("completed blob is partial")
	}
	if has, err := restarted.Has(ctx, info.Hash); err != nil || !has {
		t.Fatalf("has = %v, %v", has, err)
	}
	if read := readAll(t, restarted, info.Hash); !bytes.Equal(read, data) {
		t.Fatal("content differs from the source")
	}
	if err := restarted.WriteChunk(ctx, info.Hash, 0, chunk(0)); !errors.Is(err, ErrComplete) {
		t.Fatalf("write chunk to a complete blob: %v, want ErrComplete", err)
	}
	if _, err := restarted.Complete(ctx, info.Hash); err != nil {
		t.Fatalf("complete a complete blob: %v", err)
	}
	if missing, err := restarted.Missing(ctx, info.Hash); err != nil || len(missing) != 0 {
		t.Fatalf("missing = %v, %v, want none", missing, err)
	}
}

func TestCreatePartialValidatesTheTree(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, DefaultLimitDesktop)
	data := randomBytes(t, ChunkSize+5)
	info := mustPut(t, s, data)
	tree, err := s.Tree(ctx, info.Hash)
	if err != nil {
		t.Fatalf("read tree: %v", err)
	}

	other := newTestStore(t, DefaultLimitDesktop)
	if _, err := other.CreatePartial(ctx, info.Hash, info.Size, tree[:len(tree)-1]); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("short tree: %v, want ErrBadFormat", err)
	}
	if _, err := other.CreatePartial(ctx, info.Hash, 2*ChunkSize, tree); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("tree of another size: %v, want ErrBadFormat", err)
	}
	if _, err := other.CreatePartial(ctx, [32]byte{}, info.Size, tree); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("zero hash: %v, want ErrBadFormat", err)
	}
	if _, err := other.CreatePartial(ctx, info.Hash, info.Size, nil); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("empty tree: %v, want ErrBadFormat", err)
	}

	started, err := other.CreatePartial(ctx, info.Hash, info.Size, tree)
	if err != nil {
		t.Fatalf("create partial: %v", err)
	}
	if !started.Partial || started.Chunks != info.Chunks {
		t.Fatalf("started blob is %+v, want %d partial chunks", started, info.Chunks)
	}

	// A chunk of other content with the same length does not match the tree.
	wrong := slices.Clone(data[:ChunkSize])
	wrong[0] ^= 0xff
	if err := other.WriteChunk(ctx, info.Hash, 0, wrong); !errors.Is(err, ErrChunkMismatch) {
		t.Fatalf("write wrong chunk: %v, want ErrChunkMismatch", err)
	}
	if err := other.WriteChunk(ctx, info.Hash, info.Chunks, wrong[:1]); !errors.Is(err, ErrChunkRange) {
		t.Fatalf("write chunk out of range: %v, want ErrChunkRange", err)
	}
	if missing, err := other.Missing(ctx, info.Hash); err != nil || len(missing) != info.Chunks {
		t.Fatalf("missing = %v, %v, want %d chunks", missing, err, info.Chunks)
	}
}

func TestCompleteBlobRejectsPartialStart(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, DefaultLimitDesktop)
	data := randomBytes(t, 10)
	info := mustPut(t, s, data)

	tree, err := s.Tree(ctx, info.Hash)
	if err != nil {
		t.Fatalf("read tree: %v", err)
	}
	if _, err := s.CreatePartial(ctx, info.Hash, info.Size, tree); !errors.Is(err, ErrComplete) {
		t.Fatalf("create partial for a complete blob: %v, want ErrComplete", err)
	}
}
