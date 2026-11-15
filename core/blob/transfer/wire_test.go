package transfer

import (
	"bytes"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/lebe-dev/unison/core/blob"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testHash returns a hash of byte value b.
func testHash(b byte) [32]byte {
	var hash [32]byte
	for i := range hash {
		hash[i] = b
	}
	return hash
}

func TestParseRequest(t *testing.T) {
	hash := testHash(1)
	raw := hash[:]

	tests := []struct {
		name    string
		header  *wirev1.StreamHeader
		msg     *wirev1.BlobRequest
		want    request
		wantErr error
	}{
		{
			name:   "range",
			header: &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:    &wirev1.BlobRequest{Hash: raw, FromChunk: 2, ToChunk: 5},
			want:   request{hash: hash, from: 2, to: 5},
		},
		{
			name:   "single chunk",
			header: &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:    &wirev1.BlobRequest{Hash: raw, FromChunk: 0, ToChunk: 0},
			want:   request{hash: hash},
		},
		{
			name:   "range at the limit",
			header: &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:    &wirev1.BlobRequest{Hash: raw, ToChunk: MaxRangeChunks - 1},
			want:   request{hash: hash, to: MaxRangeChunks - 1},
		},
		{
			name:   "tree only",
			header: &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:    &wirev1.BlobRequest{Hash: raw, TreeOnly: true},
			want:   request{hash: hash, treeOnly: true},
		},
		{
			name:   "share",
			header: &wirev1.StreamHeader{PayloadType: PayloadRequest, TransferId: "share-1"},
			msg:    &wirev1.BlobRequest{Hash: raw},
			want:   request{hash: hash, transfer: "share-1"},
		},
		{
			name:    "short hash",
			header:  &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:     &wirev1.BlobRequest{Hash: raw[:31]},
			wantErr: ErrRequest,
		},
		{
			name:    "zero hash",
			header:  &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:     &wirev1.BlobRequest{Hash: make([]byte, 32)},
			wantErr: ErrRequest,
		},
		{
			name:    "reversed range",
			header:  &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:     &wirev1.BlobRequest{Hash: raw, FromChunk: 5, ToChunk: 2},
			wantErr: ErrRequest,
		},
		{
			name:    "range above the limit",
			header:  &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:     &wirev1.BlobRequest{Hash: raw, ToChunk: MaxRangeChunks},
			wantErr: ErrRequest,
		},
		{
			name:    "chunk index above int",
			header:  &wirev1.StreamHeader{PayloadType: PayloadRequest},
			msg:     &wirev1.BlobRequest{Hash: raw, FromChunk: math.MaxUint32, ToChunk: math.MaxUint32},
			wantErr: ErrRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRequest(test.header, test.msg)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error is %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got != test.want {
				t.Fatalf("request is %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestRequestRangeOf(t *testing.T) {
	tests := []struct {
		name    string
		req     request
		chunks  int
		from    int
		to      int
		wantErr error
	}{
		{name: "inside", req: request{from: 1, to: 3}, chunks: 4, from: 1, to: 3},
		{name: "last chunk", req: request{from: 3, to: 3}, chunks: 4, from: 3, to: 3},
		{name: "from outside", req: request{from: 4, to: 4}, chunks: 4, wantErr: blob.ErrChunkRange},
		{name: "to outside", req: request{from: 0, to: 4}, chunks: 4, wantErr: blob.ErrChunkRange},
		{name: "empty blob", req: request{}, chunks: 0, wantErr: blob.ErrChunkRange},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			from, to, err := test.req.rangeOf(test.chunks)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error is %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("range: %v", err)
			}
			if from != test.from || to != test.to {
				t.Fatalf("range is %d..%d, want %d..%d", from, to, test.from, test.to)
			}
		})
	}
}

func TestCheckTree(t *testing.T) {
	hash := testHash(1)
	other := testHash(2)
	size := int64(2 * blob.ChunkSize)
	valid := &wirev1.BlobTree{
		Hash:   hash[:],
		Size:   uint64(size),
		Chunks: 2,
		Tree:   make([]byte, treeSize(2)),
	}

	tests := []struct {
		name    string
		tree    *wirev1.BlobTree
		wantErr error
	}{
		{name: "valid", tree: valid},
		{name: "missing", tree: nil, wantErr: ErrTree},
		{
			name:    "another blob",
			tree:    &wirev1.BlobTree{Hash: other[:], Size: uint64(size), Chunks: 2, Tree: make([]byte, treeSize(2))},
			wantErr: ErrTree,
		},
		{
			name:    "short hash",
			tree:    &wirev1.BlobTree{Hash: hash[:31], Size: uint64(size), Chunks: 2, Tree: make([]byte, treeSize(2))},
			wantErr: ErrTree,
		},
		{
			name:    "chunk count",
			tree:    &wirev1.BlobTree{Hash: hash[:], Size: uint64(size), Chunks: 3, Tree: make([]byte, treeSize(2))},
			wantErr: ErrTree,
		},
		{
			name:    "tree length",
			tree:    &wirev1.BlobTree{Hash: hash[:], Size: uint64(size), Chunks: 2, Tree: make([]byte, treeSize(2)-1)},
			wantErr: ErrTree,
		},
		{
			name:    "huge blob",
			tree:    &wirev1.BlobTree{Hash: hash[:], Size: math.MaxUint64, Chunks: 2, Tree: make([]byte, treeSize(2))},
			wantErr: ErrTree,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, tree, err := checkTree(test.tree, hash)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error is %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			if got != size || !bytes.Equal(tree, test.tree.GetTree()) {
				t.Fatalf("tree is %d bytes of %d, want %d of %d", len(tree), got, len(test.tree.GetTree()), size)
			}
		})
	}
}

func TestSplitRanges(t *testing.T) {
	tests := []struct {
		name   string
		chunks []int
		size   int
		want   [][]int
	}{
		{name: "empty", chunks: nil, size: 2},
		{name: "single", chunks: []int{3}, size: 4, want: [][]int{{3}}},
		{name: "run capped", chunks: []int{0, 1, 2, 3}, size: 2, want: [][]int{{0, 1}, {2, 3}}},
		{name: "gap", chunks: []int{0, 1, 4}, size: 4, want: [][]int{{0, 1}, {4}}},
		{name: "one chunk per range", chunks: []int{5, 6, 7}, size: 1, want: [][]int{{5}, {6}, {7}}},
		{name: "unbounded", chunks: []int{1, 2, 3}, size: 0, want: [][]int{{1}, {2}, {3}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := splitRanges(test.chunks, test.size); !equalRanges(got, test.want) {
				t.Fatalf("ranges are %v, want %v", got, test.want)
			}
		})
	}
}

func TestStride(t *testing.T) {
	jobs := [][]int{{0}, {1}, {2}, {3}, {4}}
	if got := stride(jobs, 0, 2); !equalRanges(got, [][]int{{0}, {2}, {4}}) {
		t.Fatalf("first worker got %v", got)
	}
	if got := stride(jobs, 1, 2); !equalRanges(got, [][]int{{1}, {3}}) {
		t.Fatalf("second worker got %v", got)
	}
	if got := stride(jobs, 2, 2); !equalRanges(got, [][]int{{2}, {4}}) {
		t.Fatalf("third worker got %v", got)
	}
	if got := stride(jobs, 0, 6); !equalRanges(got, [][]int{{0}}) {
		t.Fatalf("worker of six got %v", got)
	}
}

func TestChunkLen(t *testing.T) {
	tests := []struct {
		size  int64
		index int
		want  int
	}{
		{size: 10, index: 0, want: 10},
		{size: blob.ChunkSize, index: 0, want: blob.ChunkSize},
		{size: blob.ChunkSize + 1, index: 0, want: blob.ChunkSize},
		{size: blob.ChunkSize + 1, index: 1, want: 1},
		{size: 2 * blob.ChunkSize, index: 1, want: blob.ChunkSize},
	}

	for _, test := range tests {
		if got := chunkLen(test.size, test.index); got != test.want {
			t.Fatalf("chunk %d of %d bytes is %d, want %d", test.index, test.size, got, test.want)
		}
	}
}

func TestTreeSize(t *testing.T) {
	tests := []struct {
		chunks int64
		want   int64
	}{
		{chunks: 0, want: 8},
		{chunks: 1, want: 8},
		{chunks: 2, want: 72},
		{chunks: 3, want: 136},
	}

	for _, test := range tests {
		if got := treeSize(test.chunks); got != test.want {
			t.Fatalf("tree of %d chunks is %d bytes, want %d", test.chunks, got, test.want)
		}
	}
}

func TestReplyError(t *testing.T) {
	tests := []struct {
		name    string
		reply   *wirev1.BlobReply
		wantErr error
	}{
		{name: "no refusal", reply: &wirev1.BlobReply{}},
		{
			name:    "denied",
			reply:   errorReply(wirev1.BlobStatus_BLOB_STATUS_DENIED),
			wantErr: ErrDenied,
		},
		{
			name:    "missing",
			reply:   errorReply(wirev1.BlobStatus_BLOB_STATUS_MISSING),
			wantErr: blob.ErrNotFound,
		},
		{
			name:    "unavailable",
			reply:   errorReply(wirev1.BlobStatus_BLOB_STATUS_UNAVAILABLE),
			wantErr: ErrReply,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := replyError(test.reply)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("error is %v, want none", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error is %v, want %v", err, test.wantErr)
			}
		})
	}
}

// equalRanges reports whether two chunk ranges are equal.
func equalRanges(a, b [][]int) bool {
	return slices.EqualFunc(a, b, slices.Equal[[]int])
}
