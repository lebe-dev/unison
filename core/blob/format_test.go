package blob

import (
	"errors"
	"testing"

	"lukechampine.com/blake3/bao"
)

func TestMigrationsUseTheBlobBlock(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations")
	}
	if first := migrations[0].Version; first != migrationsBase+1 {
		t.Fatalf("first migration version is %d, want %d", first, migrationsBase+1)
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	in := header{size: 3*ChunkSize + 17, chunks: 4, root: [32]byte{1, 2, 3}}
	out, err := decodeHeader(in.encode())
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if out != in {
		t.Fatalf("header is %+v, want %+v", out, in)
	}
}

func TestDecodeHeaderRejectsBadFiles(t *testing.T) {
	valid := header{size: ChunkSize, chunks: 1, root: [32]byte{4}}

	cases := []struct {
		name string
		raw  []byte
	}{
		{"short", valid.encode()[:headerSize-1]},
		{"version", flipByte(valid.encode(), 8, 2)},
		{"magic", flipByte(valid.encode(), 0, 'X')},
		{"chunk_size", flipByte(valid.encode(), 21, 1)},
		{"chunks", flipByte(valid.encode(), 26, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodeHeader(c.raw); !errors.Is(err, ErrBadFormat) {
				t.Fatalf("decode header: %v, want ErrBadFormat", err)
			}
		})
	}
}

func TestLayoutMatchesTheBaoEncoding(t *testing.T) {
	for _, size := range []int64{0, 1, ChunkSize, ChunkSize + 1, 5 * ChunkSize} {
		lay := newLayout(size)
		if want := int64(bao.EncodedSize(int(size), baoGroup, true)); lay.treeSize() != want {
			t.Fatalf("tree of %d bytes is %d bytes, want %d", size, lay.treeSize(), want)
		}
		if want := chunksFor(size); lay.chunks != want {
			t.Fatalf("chunks of %d bytes is %d, want %d", size, lay.chunks, want)
		}
		var total int64
		for i := range lay.chunks {
			total += int64(lay.chunkLen(i)) + chunkOverhead
		}
		if got, want := lay.fileSize(), lay.chunksOffset()+total; got != want {
			t.Fatalf("file of %d bytes is %d, want %d", size, got, want)
		}
	}
}

func TestChunkBitmap(t *testing.T) {
	bitmap := make([]byte, bitmapLen(9))
	for _, index := range []int{0, 3, 8} {
		setBit(bitmap, index)
	}
	if want := []int{1, 2, 4, 5, 6, 7}; !equalInts(missingChunks(bitmap, 9), want) {
		t.Fatalf("missing = %v, want %v", missingChunks(bitmap, 9), want)
	}
	if missing := missingChunks(nil, 3); len(missing) != 3 {
		t.Fatalf("missing = %v, want all chunks", missing)
	}
}

// flipByte returns raw with the byte at off replaced.
func flipByte(raw []byte, off int, value byte) []byte {
	out := make([]byte, len(raw))
	copy(out, raw)
	out[off] = value
	return out
}

// equalInts reports whether two index lists are equal.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
