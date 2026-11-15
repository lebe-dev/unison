package transfer

import (
	"bytes"
	"fmt"
	"math"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/blob"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// treePrefix is the size of the length prefix of an outboard Bao tree.
const treePrefix = 8

// request is a validated blob request of a peer.
type request struct {
	hash     [32]byte
	from     int
	to       int
	treeOnly bool
	// transfer is the share the request belongs to, empty for a blob of the
	// history.
	transfer string
}

// parseRequest validates the request frame of a request stream.
func parseRequest(header *wirev1.StreamHeader, msg *wirev1.BlobRequest) (request, error) {
	req := request{transfer: header.GetTransferId(), treeOnly: msg.GetTreeOnly()}
	if len(msg.GetHash()) != 32 {
		return request{}, fmt.Errorf("%w: hash of %d bytes", ErrRequest, len(msg.GetHash()))
	}
	copy(req.hash[:], msg.GetHash())
	if req.hash == ([32]byte{}) {
		return request{}, fmt.Errorf("%w: zero hash", ErrRequest)
	}
	if req.treeOnly {
		return req, nil
	}

	from, to := uint64(msg.GetFromChunk()), uint64(msg.GetToChunk())
	if from > math.MaxInt32 || to < from {
		return request{}, fmt.Errorf("%w: chunks %d..%d", ErrRequest, from, to)
	}
	if to-from >= MaxRangeChunks {
		return request{}, fmt.Errorf("%w: range of %d chunks", ErrRequest, to-from+1)
	}
	req.from, req.to = int(from), int(to)
	return req, nil
}

// rangeOf returns the chunk range of the request inside a blob of chunks
// chunks.
func (r request) rangeOf(chunks int) (int, int, error) {
	if chunks <= 0 || r.from >= chunks || r.to >= chunks {
		return 0, 0, fmt.Errorf("%w: chunks %d..%d of %d", blob.ErrChunkRange, r.from, r.to, chunks)
	}
	return r.from, r.to, nil
}

// checkTree validates a hash tree a peer sent and returns the content size and
// the tree bytes.
func checkTree(tree *wirev1.BlobTree, hash [32]byte) (int64, []byte, error) {
	if tree == nil {
		return 0, nil, fmt.Errorf("%w: no tree", ErrTree)
	}
	if len(tree.GetHash()) != 32 || !bytes.Equal(tree.GetHash(), hash[:]) {
		return 0, nil, fmt.Errorf("%w: tree of another blob", ErrTree)
	}

	size := tree.GetSize()
	if size > math.MaxInt64 {
		return 0, nil, fmt.Errorf("%w: size %d", ErrTree, size)
	}
	chunks := chunksFor(int64(size))
	if treeSize(chunks) > MaxTreeBytes {
		return 0, nil, fmt.Errorf("%w: tree of %d chunks", ErrTree, chunks)
	}
	if int64(tree.GetChunks()) != chunks {
		return 0, nil, fmt.Errorf("%w: %d chunks for %d bytes", ErrTree, tree.GetChunks(), size)
	}
	if int64(len(tree.GetTree())) != treeSize(chunks) {
		return 0, nil, fmt.Errorf("%w: tree of %d bytes", ErrTree, len(tree.GetTree()))
	}
	return int64(size), tree.GetTree(), nil
}

// chunkHash returns the BLAKE3 hash of one plaintext chunk.
func chunkHash(data []byte) [32]byte {
	return blake3.Sum256(data)
}

// chunksFor returns the number of chunks of content of size bytes.
func chunksFor(size int64) int64 {
	if size <= 0 {
		return 0
	}
	return (size + blob.ChunkSize - 1) / blob.ChunkSize
}

// treeSize returns the size of the outboard tree of chunks chunks.
func treeSize(chunks int64) int64 {
	if chunks == 0 {
		return treePrefix
	}
	return treePrefix + 64*(chunks-1)
}

// chunkLen returns the plaintext length of the chunk at index of a blob of size
// bytes.
func chunkLen(size int64, index int) int {
	if int64(index+1)*blob.ChunkSize <= size {
		return blob.ChunkSize
	}
	return int(size - int64(index)*blob.ChunkSize)
}

// requestHeader returns the header of an outgoing blob request stream.
func requestHeader(transferID string) *wirev1.StreamHeader {
	return &wirev1.StreamHeader{PayloadType: PayloadRequest, TransferId: transferID}
}

// treeReply returns the frame that carries the hash tree of a blob.
func treeReply(info blob.Info, tree []byte) *wirev1.BlobReply {
	return &wirev1.BlobReply{Body: &wirev1.BlobReply_Tree{Tree: &wirev1.BlobTree{
		Hash:   info.Hash[:],
		Size:   uint64(info.Size),
		Chunks: uint32(info.Chunks),
		Tree:   tree,
	}}}
}

// chunkReply returns the frame that carries one chunk.
func chunkReply(index int, data, sum []byte) *wirev1.BlobReply {
	return &wirev1.BlobReply{Body: &wirev1.BlobReply_Chunk{Chunk: &wirev1.BlobChunk{
		Index:     uint32(index),
		Data:      data,
		ChunkHash: sum,
	}}}
}

// errorReply returns the frame that refuses a request.
func errorReply(status wirev1.BlobStatus) *wirev1.BlobReply {
	return &wirev1.BlobReply{Body: &wirev1.BlobReply_Error{Error: &wirev1.BlobError{Status: status}}}
}

// replyError returns the reason a source refused the request, or nil.
func replyError(reply *wirev1.BlobReply) error {
	refusal := reply.GetError()
	if refusal == nil {
		return nil
	}
	switch refusal.GetStatus() {
	case wirev1.BlobStatus_BLOB_STATUS_DENIED:
		return ErrDenied
	case wirev1.BlobStatus_BLOB_STATUS_MISSING:
		return blob.ErrNotFound
	default:
		return fmt.Errorf("%w: source cannot serve the blob", ErrReply)
	}
}

// stride returns the items of jobs that belong to worker i of n.
func stride(jobs [][]int, i, n int) [][]int {
	if i >= len(jobs) {
		return nil
	}
	out := make([][]int, 0, (len(jobs)-i+n-1)/n)
	for j := i; j < len(jobs); j += n {
		out = append(out, jobs[j])
	}
	return out
}

// splitRanges groups missing chunks into runs of consecutive indices of at most
// size chunks.
func splitRanges(chunks []int, size int) [][]int {
	if size <= 0 {
		size = 1
	}
	out := make([][]int, 0, len(chunks)/size+1)
	for start := 0; start < len(chunks); {
		end := start + 1
		for end < len(chunks) && chunks[end] == chunks[end-1]+1 && end-start < size {
			end++
		}
		out = append(out, chunks[start:end])
		start = end
	}
	return out
}
