package blob

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"

	"golang.org/x/crypto/chacha20poly1305"
	"lukechampine.com/blake3/bao"

	"github.com/lebe-dev/unison/core/store"
)

// File layout constants.
const (
	// headerSize is the fixed size of the blob header.
	headerSize = 64
	// formatVersion is the version of the file layout.
	formatVersion = 1
	// chunkOverhead is the nonce and the tag of a sealed chunk.
	chunkOverhead = chacha20poly1305.NonceSizeX + chacha20poly1305.Overhead
	// baoGroup is the log2 of the tree group size in BLAKE3 chunks, which makes
	// one group one stored chunk.
	baoGroup = 10
	// treeHeaderSize is the data length prefix of the Bao tree.
	treeHeaderSize = 8
	// tempPrefix marks files that are not yet a stored blob.
	tempPrefix = ".tmp-"
	// chunkTable names the sealed chunks for the sealer binding.
	chunkTable = "blob"
)

// magic marks a blob file.
var magic = [8]byte{'U', 'N', 'I', 'S', 'O', 'N', 'B', 'L'}

// A blob file holds the fixed header, the hash tree and the sealed chunks:
//
//	header | tree | chunk 0 | chunk 1 | ... | chunk n-1
//
// Every chunk but the last one is ChunkSize bytes of plaintext and
// chunkOverhead bytes of nonce and tag.
type header struct {
	size   int64
	chunks int
	root   [32]byte
}

// encode returns the on-disk form of the header.
func (h header) encode() []byte {
	b := make([]byte, headerSize)
	copy(b, magic[:])
	b[8] = formatVersion
	binary.BigEndian.PutUint64(b[12:20], uint64(h.size))
	binary.BigEndian.PutUint32(b[20:24], ChunkSize)
	binary.BigEndian.PutUint32(b[24:28], uint32(h.chunks))
	copy(b[28:60], h.root[:])
	return b
}

// decodeHeader parses the on-disk form of the header.
func decodeHeader(b []byte) (header, error) {
	if len(b) < headerSize {
		return header{}, fmt.Errorf("%w: header is %d bytes", ErrBadFormat, len(b))
	}
	if !bytes.Equal(b[:8], magic[:]) {
		return header{}, fmt.Errorf("%w: bad magic", ErrBadFormat)
	}
	if version := b[8]; version != formatVersion {
		return header{}, fmt.Errorf("%w: version %d", ErrBadFormat, version)
	}
	size := int64(binary.BigEndian.Uint64(b[12:20]))
	if size < 0 {
		return header{}, fmt.Errorf("%w: size %d", ErrBadFormat, size)
	}
	if chunkSize := binary.BigEndian.Uint32(b[20:24]); chunkSize != ChunkSize {
		return header{}, fmt.Errorf("%w: chunk size %d", ErrBadFormat, chunkSize)
	}
	chunks := int(binary.BigEndian.Uint32(b[24:28]))
	if chunks != chunksFor(size) {
		return header{}, fmt.Errorf("%w: %d chunks for %d bytes", ErrBadFormat, chunks, size)
	}
	h := header{size: size, chunks: chunks}
	copy(h.root[:], b[28:60])
	return h, nil
}

// layout locates the parts of a blob file of a known size.
type layout struct {
	size   int64
	chunks int
}

// newLayout returns the layout of content of size bytes.
func newLayout(size int64) layout {
	return layout{size: size, chunks: chunksFor(size)}
}

// treeOffset returns the offset of the hash tree.
func (l layout) treeOffset() int64 {
	return headerSize
}

// treeSize returns the size of the Bao tree: a length prefix and one chaining
// value pair per inner node.
func (l layout) treeSize() int64 {
	if l.chunks == 0 {
		return treeHeaderSize
	}
	return treeHeaderSize + int64(2*l.chunks-2)*32
}

// chunksOffset returns the offset of the first sealed chunk.
func (l layout) chunksOffset() int64 {
	return l.treeOffset() + l.treeSize()
}

// chunkOffset returns the offset of the sealed chunk at index.
func (l layout) chunkOffset(index int) int64 {
	return l.chunksOffset() + int64(index)*(ChunkSize+chunkOverhead)
}

// chunkLen returns the plaintext length of the chunk at index.
func (l layout) chunkLen(index int) int {
	if index < l.chunks-1 {
		return ChunkSize
	}
	return int(l.size - int64(index)*ChunkSize)
}

// fileSize returns the size of the whole file.
func (l layout) fileSize() int64 {
	if l.chunks == 0 {
		return l.chunksOffset()
	}
	return l.chunkOffset(l.chunks-1) + int64(l.chunkLen(l.chunks-1)) + chunkOverhead
}

// chunksFor returns the number of chunks of content of size bytes.
func chunksFor(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + ChunkSize - 1) / ChunkSize)
}

// chunkRowKey binds a sealed chunk to the blob hash and the chunk index.
func chunkRowKey(hash [32]byte, index int) string {
	return hashHex(hash) + "/" + strconv.Itoa(index)
}

// sectionWriter writes at offsets of a region of a file.
type sectionWriter struct {
	w    io.WriterAt
	base int64
}

// WriteAt writes p at off of the region.
func (s sectionWriter) WriteAt(p []byte, off int64) (int, error) {
	return s.w.WriteAt(p, s.base+off)
}

// chunkFile is an open blob file.
type chunkFile struct {
	file   *os.File
	layout layout
	root   [32]byte
}

// openChunkFile prepares an open file for chunk operations.
func openChunkFile(f *os.File, lay layout, root [32]byte) *chunkFile {
	return &chunkFile{file: f, layout: lay, root: root}
}

// verify checks a plaintext chunk against the hash tree of the blob.
func (c *chunkFile) verify(index int, data []byte) error {
	if index < 0 || index >= c.layout.chunks {
		return fmt.Errorf("%w: %d of %d", ErrChunkRange, index, c.layout.chunks)
	}
	want := c.layout.chunkLen(index)
	if len(data) != want {
		return fmt.Errorf("%w: chunk %d is %d bytes, want %d", ErrChunkMismatch, index, len(data), want)
	}

	offset := uint64(int64(index) * ChunkSize)
	outboard := io.NewSectionReader(c.file, c.layout.treeOffset(), c.layout.treeSize())

	// The tree path of the chunk is extracted first and then decoded against
	// the root, which yields the same proof a peer sends on its own.
	var slice bytes.Buffer
	if err := bao.ExtractSlice(&slice, bytes.NewReader(data), outboard, baoGroup, offset, uint64(want)); err != nil {
		return fmt.Errorf("%w: chunk %d: %w", ErrChunkMismatch, index, err)
	}
	ok, err := bao.DecodeSlice(io.Discard, bytes.NewReader(slice.Bytes()), baoGroup, offset, uint64(want), c.root)
	if err != nil {
		return fmt.Errorf("%w: chunk %d: %w", ErrChunkMismatch, index, err)
	}
	if !ok {
		return fmt.Errorf("%w: chunk %d", ErrChunkMismatch, index)
	}
	return nil
}

// read returns the plaintext chunk at index.
func (c *chunkFile) read(sealer *store.Sealer, index int) ([]byte, error) {
	if index < 0 || index >= c.layout.chunks {
		return nil, fmt.Errorf("%w: %d of %d", ErrChunkRange, index, c.layout.chunks)
	}

	sealed := make([]byte, c.layout.chunkLen(index)+chunkOverhead)
	if _, err := c.file.ReadAt(sealed, c.layout.chunkOffset(index)); err != nil {
		return nil, fmt.Errorf("blob: read chunk %d: %w", index, err)
	}
	data, err := sealer.Open(chunkTable, chunkRowKey(c.root, index), sealed)
	if err != nil {
		return nil, fmt.Errorf("%w: chunk %d: %w", ErrCorrupt, index, err)
	}
	if err := c.verify(index, data); err != nil {
		return nil, err
	}
	return data, nil
}
