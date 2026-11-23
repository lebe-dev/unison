package transfer

import (
	"errors"
	"time"
)

// PayloadRequest is the payload type of a blob request stream.
const PayloadRequest = "blob.request"

// Limits applied to a peer.
const (
	// MaxRangeChunks is the largest chunk range a peer may request at once.
	MaxRangeChunks = 64
	// MaxSources bounds the sources one fetch uses.
	MaxSources = 16
	// MaxTreeBytes bounds the hash tree a peer may send.
	MaxTreeBytes = 1 << 21

	// DefaultRangeChunks is the chunk range a fetch requests when nothing else
	// is set.
	DefaultRangeChunks = 8
	// DefaultPerPeerRequests is the number of range requests a fetch keeps open
	// to one source when nothing else is set.
	DefaultPerPeerRequests = 2

	// DefaultHaveTimeout bounds the answer to a blob availability query.
	DefaultHaveTimeout = 5 * time.Second
	// DefaultFrameTimeout bounds one frame read or write of a blob stream.
	DefaultFrameTimeout = 30 * time.Second
)

// Errors reported by the transfer layer.
var (
	// ErrNoSource reports that no peer offered the blob.
	ErrNoSource = errors.New("transfer: no peer has the blob")
	// ErrIncomplete reports a fetch that ended with chunks still missing.
	ErrIncomplete = errors.New("transfer: download is incomplete")
	// ErrRequest reports a blob request that is malformed or above a limit.
	ErrRequest = errors.New("transfer: invalid blob request")
	// ErrDenied reports a peer that may not fetch the blob.
	ErrDenied = errors.New("transfer: blob access denied")
	// ErrTree reports a hash tree that does not describe the blob.
	ErrTree = errors.New("transfer: hash tree does not match the blob")
	// ErrReply reports a frame a blob stream does not allow at that point.
	ErrReply = errors.New("transfer: unexpected blob reply")
	// ErrSize reports a blob larger than the size its peer announced.
	ErrSize = errors.New("transfer: blob is larger than announced")
	// ErrChunk reports a chunk that does not match its own hash or the tree.
	ErrChunk = errors.New("transfer: corrupted blob chunk")
)
