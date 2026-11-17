package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/internal/observability"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FetchOptions configures NewFetcher.
type FetchOptions struct {
	// Blobs is the store the fetcher writes to. It is required.
	Blobs *blob.Store
	// RangeChunks is the largest chunk range one request asks for. It is capped
	// at MaxRangeChunks, and DefaultRangeChunks is used when zero.
	RangeChunks int
	// PerPeerRequests is the number of range requests the fetcher keeps open to
	// one source. DefaultPerPeerRequests is used when zero.
	PerPeerRequests int
	// HaveTimeout bounds the answer to a blob availability query.
	// DefaultHaveTimeout is used when zero.
	HaveTimeout time.Duration
	// FrameTimeout bounds one frame read or write of a blob stream.
	// DefaultFrameTimeout is used when zero.
	FrameTimeout time.Duration
	// Logger receives transfer events. Nothing is logged when nil.
	Logger *slog.Logger
}

// Fetcher downloads blobs from peers.
type Fetcher struct {
	blobs        *blob.Store
	rangeChunks  int
	perPeer      int
	haveTimeout  time.Duration
	frameTimeout time.Duration
	log          *slog.Logger

	locks hashLocks

	mu      sync.Mutex
	waiters map[[32]byte][]*haveWaiter
}

// NewFetcher prepares a fetcher.
func NewFetcher(opts FetchOptions) (*Fetcher, error) {
	if opts.Blobs == nil {
		return nil, errors.New("transfer: blob store is required")
	}
	f := &Fetcher{
		blobs:        opts.Blobs,
		rangeChunks:  opts.RangeChunks,
		perPeer:      opts.PerPeerRequests,
		haveTimeout:  opts.HaveTimeout,
		frameTimeout: opts.FrameTimeout,
		log:          opts.Logger,
		waiters:      make(map[[32]byte][]*haveWaiter),
	}
	if f.rangeChunks <= 0 {
		f.rangeChunks = DefaultRangeChunks
	}
	f.rangeChunks = min(f.rangeChunks, MaxRangeChunks)
	if f.perPeer <= 0 {
		f.perPeer = DefaultPerPeerRequests
	}
	if f.haveTimeout <= 0 {
		f.haveTimeout = DefaultHaveTimeout
	}
	if f.frameTimeout <= 0 {
		f.frameTimeout = DefaultFrameTimeout
	}
	if f.log == nil {
		f.log = slog.New(slog.DiscardHandler)
	}
	return f, nil
}

// Register registers the handler of the answers to the availability queries the
// fetcher sends.
func (f *Fetcher) Register(sess *session.Session) error {
	peer := sess.DeviceID()
	return sess.Handle(session.KindHaveBlobResp, func(_ context.Context, msg *wirev1.ControlMessage) error {
		f.answer(peer, msg.GetHaveBlobResp())
		return nil
	})
}

// FetchRequest describes one download.
type FetchRequest struct {
	// Hash is the BLAKE3 hash of the content.
	Hash [32]byte
	// Peers are the sessions a source may be.
	Peers []Peer
	// TransferID ties the blob to a share. It is empty for a blob of the
	// history.
	TransferID string
	// PayloadType selects the request stream handler that serves the blob.
	// PayloadRequest is used when empty.
	PayloadType string
	// Trusted skips the availability query: every peer is taken as a source.
	// It is set for a blob a peer has just offered.
	Trusted bool
	// Progress is called after every stored chunk, one call at a time. It runs
	// on the fetch goroutines and must not fetch the same blob.
	Progress func(Progress)
}

// Progress reports one stored chunk of a download.
type Progress struct {
	// Hash is the BLAKE3 hash of the content.
	Hash [32]byte
	// Chunks is the number of chunks of the blob.
	Chunks int
	// Received is the number of chunks the store holds.
	Received int
	// Bytes is the number of bytes this fetch stored.
	Bytes int64
	// Source is the peer that sent the chunk.
	Source model.DeviceID
	// Index is the chunk that was just stored.
	Index int
}

// Result describes a finished download.
type Result struct {
	// Info is the metadata of the stored blob.
	Info blob.Info
	// Sources are the peers that stored at least one chunk.
	Sources []model.DeviceID
	// Excluded are the peers that were dropped for sending bad data.
	Excluded []model.DeviceID
	// Ranges is the number of range requests.
	Ranges int
	// Bytes is the number of bytes this fetch stored.
	Bytes int64
}

// Fetch downloads a blob the local store does not hold yet. Chunks already
// stored are kept, so a call after an interruption resumes from the missing
// ones. A peer that sends corrupt or unexpected data is dropped and its chunks
// are requested from the remaining peers.
func (f *Fetcher) Fetch(ctx context.Context, request FetchRequest) (Result, error) {
	if request.Hash == ([32]byte{}) {
		return Result{}, fmt.Errorf("%w: zero hash", ErrRequest)
	}
	peers := uniquePeers(request.Peers)
	if len(peers) == 0 {
		return Result{}, ErrNoSource
	}

	// One fetch per blob: a second caller waits and then finds the chunks the
	// first one stored.
	release := f.locks.lock(request.Hash)
	defer release()

	if has, err := f.blobs.Has(ctx, request.Hash); err != nil {
		return Result{}, err
	} else if has {
		info, err := f.blobs.Stat(ctx, request.Hash)
		if err != nil {
			return Result{}, err
		}
		return Result{Info: info}, nil
	}

	fl := &fetchState{fetcher: f, request: request, sources: f.query(ctx, request, peers)}
	return fl.run(ctx)
}

// payloadType returns the payload type of the request streams of the fetch.
func (fl *fetchState) payloadType() string {
	if fl.request.PayloadType == "" {
		return PayloadRequest
	}
	return fl.request.PayloadType
}

// read decodes one frame of the stream.
func (f *Fetcher) read(ctx context.Context, stream *session.Request, msg proto.Message) error {
	ctx, cancel := context.WithTimeout(ctx, f.frameTimeout)
	defer cancel()
	return stream.Read(ctx, msg)
}

// write sends one frame of the stream.
func (f *Fetcher) write(ctx context.Context, stream *session.Request, msg proto.Message) error {
	ctx, cancel := context.WithTimeout(ctx, f.frameTimeout)
	defer cancel()
	return stream.Write(ctx, msg)
}

// end reads the last frame of a request stream. The peer closes the stream after
// its answer, and a stream releases its slot of the connection only when both
// directions are closed, so a fetch with many ranges stops when one is left
// open.
func (f *Fetcher) end(ctx context.Context, stream *session.Request) error {
	err := f.read(ctx, stream, &wirev1.BlobReply{})
	switch {
	case err == nil:
		return fmt.Errorf("%w: frame after the answer", ErrReply)
	case errors.Is(err, io.EOF):
		return nil
	default:
		return err
	}
}

// query asks the peers whether they store the blob and returns the ones that
// answered yes. A trusted request skips the question: the peer has just offered
// the blob.
func (f *Fetcher) query(ctx context.Context, request FetchRequest, peers []Peer) *sources {
	srcs := newSources()
	if request.Trusted {
		for _, peer := range peers {
			srcs.add(source{peer: peer, id: peer.DeviceID()})
		}
		return srcs
	}

	have := make([]bool, len(peers))
	var wg sync.WaitGroup
	for i, peer := range peers {
		wg.Add(1)
		go func(i int, peer Peer) {
			defer wg.Done()
			defer observability.Recover()
			ok, err := f.have(ctx, peer, request.Hash)
			if err != nil {
				f.log.Debug("blob query failed", "peer", peer.DeviceID(), "hash", hashHex(request.Hash), "error", err)
				return
			}
			have[i] = ok
		}(i, peer)
	}
	wg.Wait()

	for i, ok := range have {
		if ok {
			srcs.add(source{peer: peers[i], id: peers[i].DeviceID()})
		}
	}
	return srcs
}

// have asks one peer whether it stores the blob.
func (f *Fetcher) have(ctx context.Context, peer Peer, hash [32]byte) (bool, error) {
	waiter := make(chan bool, 1)
	f.watch(hash, peer.DeviceID(), waiter)
	defer f.unwatch(hash, waiter)

	ctx, cancel := context.WithTimeout(ctx, f.haveTimeout)
	defer cancel()
	msg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_HaveBlob{HaveBlob: &wirev1.HaveBlob{Hash: hash[:]}}}
	if err := peer.Send(ctx, msg); err != nil {
		return false, err
	}
	select {
	case have := <-waiter:
		return have, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// haveWaiter waits for the answer of one peer.
type haveWaiter struct {
	peer model.DeviceID
	ch   chan bool
}

// watch registers a waiter for the answer of peer.
func (f *Fetcher) watch(hash [32]byte, peer model.DeviceID, ch chan bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waiters[hash] = append(f.waiters[hash], &haveWaiter{peer: peer, ch: ch})
}

// unwatch removes the waiter of a finished query.
func (f *Fetcher) unwatch(hash [32]byte, ch chan bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.waiters[hash]
	for i, waiter := range list {
		if waiter.ch != ch {
			continue
		}
		list = append(list[:i], list[i+1:]...)
		if len(list) == 0 {
			delete(f.waiters, hash)
			return
		}
		f.waiters[hash] = list
		return
	}
}

// answer delivers an availability answer to the query of its peer.
func (f *Fetcher) answer(peer model.DeviceID, msg *wirev1.HaveBlobResp) {
	if len(msg.GetHash()) != 32 {
		return
	}
	var hash [32]byte
	copy(hash[:], msg.GetHash())

	f.mu.Lock()
	var target *haveWaiter
	for _, waiter := range f.waiters[hash] {
		if waiter.peer == peer {
			target = waiter
			break
		}
	}
	f.mu.Unlock()

	if target == nil {
		return
	}
	select {
	case target.ch <- msg.GetHave():
	default:
	}
}

// fetchState is one download.
type fetchState struct {
	fetcher *Fetcher
	request FetchRequest
	sources *sources

	info blob.Info
	tree []byte

	mu       sync.Mutex
	base     int
	received int
	bytes    int64
	ranges   int
	used     []model.DeviceID
}

// run prepares the partial blob and downloads the missing chunks.
func (fl *fetchState) run(ctx context.Context) (Result, error) {
	if len(fl.sources.available()) == 0 {
		return Result{}, ErrNoSource
	}
	if err := fl.prepare(ctx); err != nil {
		return Result{}, err
	}
	if err := fl.download(ctx); err != nil {
		return Result{}, err
	}
	info, err := fl.fetcher.blobs.Complete(ctx, fl.request.Hash)
	if err != nil {
		return Result{}, err
	}
	return fl.result(info), nil
}

// prepare creates the partial blob or resumes the one the store holds.
func (fl *fetchState) prepare(ctx context.Context) error {
	info, err := fl.fetcher.blobs.Stat(ctx, fl.request.Hash)
	switch {
	case err == nil && !info.Partial:
		fl.info = info
		return nil
	case err == nil:
		missing, err := fl.fetcher.blobs.Missing(ctx, fl.request.Hash)
		if err != nil {
			return err
		}
		if len(missing) == info.Chunks {
			// Nothing was received yet, so the stored tree is not proven. Start
			// over with a tree from the sources.
			if err := fl.fetcher.blobs.Remove(ctx, fl.request.Hash); err != nil {
				return err
			}
			return fl.start(ctx)
		}
		tree, err := fl.fetcher.blobs.Tree(ctx, fl.request.Hash)
		if err != nil {
			return err
		}
		fl.info, fl.tree = info, tree
		fl.base = info.Chunks - len(missing)
		return nil
	case !errors.Is(err, blob.ErrNotFound):
		return err
	}
	return fl.start(ctx)
}

// start creates a partial blob from the tree of the first source that proves
// it with the first chunk. A tree that does not belong to the blob cannot
// verify any chunk, so a source that sends one is dropped before the other
// sources are asked.
func (fl *fetchState) start(ctx context.Context) error {
	var last error
	for _, src := range fl.sources.available() {
		if err := ctx.Err(); err != nil {
			return err
		}
		tree, err := fl.treeRequest(ctx, src)
		if err != nil {
			last = fl.drop(src, err)
			continue
		}
		size, raw, err := checkTree(tree, fl.request.Hash)
		if err != nil {
			last = fl.drop(src, err)
			continue
		}

		info, err := fl.fetcher.blobs.CreatePartial(ctx, fl.request.Hash, size, raw)
		switch {
		case errors.Is(err, blob.ErrComplete):
			info, err = fl.fetcher.blobs.Stat(ctx, fl.request.Hash)
			if err != nil {
				return err
			}
			fl.info = info
			return nil
		case err != nil:
			last = fl.drop(src, err)
			continue
		}
		fl.info, fl.tree = info, raw

		if info.Chunks == 0 {
			// Content without chunks has no chunk to prove the tree with, so the
			// hash of the empty content is the proof.
			if chunkHash(nil) == fl.request.Hash {
				return nil
			}
			last = fl.drop(src, ErrTree)
			if err := fl.fetcher.blobs.Remove(ctx, fl.request.Hash); err != nil {
				return err
			}
			continue
		}
		if err := fl.fetchRange(ctx, src, []int{0}); err != nil {
			last = fl.drop(src, err)
			if err := fl.fetcher.blobs.Remove(ctx, fl.request.Hash); err != nil {
				return err
			}
			continue
		}
		return nil
	}

	if last == nil {
		return ErrNoSource
	}
	return last
}

// treeRequest asks one source for the hash tree of the blob.
func (fl *fetchState) treeRequest(ctx context.Context, src source) (*wirev1.BlobTree, error) {
	stream, err := src.peer.OpenRequest(ctx, requestHeader(fl.request.TransferID, fl.payloadType()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	defer stream.CancelRead()

	msg := &wirev1.BlobRequest{Hash: fl.request.Hash[:], TreeOnly: true}
	if err := fl.fetcher.write(ctx, stream, msg); err != nil {
		return nil, err
	}
	reply := &wirev1.BlobReply{}
	if err := fl.fetcher.read(ctx, stream, reply); err != nil {
		return nil, err
	}
	if err := replyError(reply); err != nil {
		return nil, err
	}
	tree := reply.GetTree()
	if tree == nil {
		return nil, fmt.Errorf("%w: tree expected", ErrReply)
	}
	if err := fl.fetcher.end(ctx, stream); err != nil {
		return nil, err
	}
	return tree, nil
}

// download requests the missing chunks from the live sources until none are
// left.
func (fl *fetchState) download(ctx context.Context) error {
	missing, err := fl.fetcher.blobs.Missing(ctx, fl.request.Hash)
	if err != nil {
		return err
	}

	for len(missing) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		live := fl.sources.available()
		if len(live) == 0 {
			return fmt.Errorf("%w: %d of %d chunks missing", ErrIncomplete, len(missing), fl.info.Chunks)
		}

		fl.round(ctx, live, splitRanges(missing, fl.fetcher.rangeChunks))
		if err := ctx.Err(); err != nil {
			return err
		}

		next, err := fl.fetcher.blobs.Missing(ctx, fl.request.Hash)
		if err != nil {
			return err
		}
		if len(next) >= len(missing) && len(fl.sources.available()) == len(live) {
			return fmt.Errorf("%w: %d of %d chunks missing", ErrIncomplete, len(next), fl.info.Chunks)
		}
		missing = next
	}
	return nil
}

// round hands the ranges to the live sources round robin. Every source keeps up
// to perPeer requests open; a source that fails is dropped with the chunks it
// did not deliver.
func (fl *fetchState) round(ctx context.Context, live []source, jobs [][]int) {
	var wg sync.WaitGroup
	for i := range live {
		assigned := stride(jobs, i, len(live))
		if len(assigned) == 0 {
			continue
		}

		var (
			dead   atomic.Bool
			cursor atomic.Int64
		)
		for range min(fl.fetcher.perPeer, len(assigned)) {
			wg.Add(1)
			go func(src source) {
				defer wg.Done()
				defer observability.Recover()
				for !dead.Load() {
					n := int(cursor.Add(1)) - 1
					if n >= len(assigned) {
						return
					}
					if err := fl.fetchRange(ctx, src, assigned[n]); err != nil {
						dead.Store(true)
						_ = fl.drop(src, err)
						return
					}
				}
			}(live[i])
		}
	}
	wg.Wait()
}

// fetchRange requests the chunks of one range and stores them.
func (fl *fetchState) fetchRange(ctx context.Context, src source, chunks []int) error {
	if len(chunks) == 0 {
		return nil
	}
	from, to := chunks[0], chunks[len(chunks)-1]

	stream, err := src.peer.OpenRequest(ctx, requestHeader(fl.request.TransferID, fl.payloadType()))
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	defer stream.CancelRead()

	fl.countRange()
	msg := &wirev1.BlobRequest{Hash: fl.request.Hash[:], FromChunk: uint32(from), ToChunk: uint32(to)}
	if err := fl.fetcher.write(ctx, stream, msg); err != nil {
		return err
	}

	// Every answer starts with the tree, so every source proves the content it
	// sends against the same hash.
	reply := &wirev1.BlobReply{}
	if err := fl.fetcher.read(ctx, stream, reply); err != nil {
		return err
	}
	if err := replyError(reply); err != nil {
		return err
	}
	if err := fl.checkTree(reply.GetTree()); err != nil {
		return err
	}

	for index := from; index <= to; index++ {
		frame := &wirev1.BlobReply{}
		if err := fl.fetcher.read(ctx, stream, frame); err != nil {
			return err
		}
		if err := replyError(frame); err != nil {
			return err
		}
		chunk := frame.GetChunk()
		if chunk == nil {
			return fmt.Errorf("%w: chunk %d expected", ErrReply, index)
		}
		if int(chunk.GetIndex()) != index {
			return fmt.Errorf("%w: chunk %d instead of %d", ErrReply, chunk.GetIndex(), index)
		}
		if err := fl.store(ctx, index, chunk); err != nil {
			return err
		}
		fl.report(src, index)
	}
	return fl.fetcher.end(ctx, stream)
}

// checkTree compares the tree of an answer with the one the fetch commits to.
func (fl *fetchState) checkTree(tree *wirev1.BlobTree) error {
	switch {
	case tree == nil:
		return fmt.Errorf("%w: tree expected", ErrTree)
	case len(tree.GetHash()) != 32 || !bytes.Equal(tree.GetHash(), fl.request.Hash[:]):
		return fmt.Errorf("%w: tree of another blob", ErrTree)
	case int64(tree.GetSize()) != fl.info.Size || int(tree.GetChunks()) != fl.info.Chunks:
		return fmt.Errorf("%w: tree of %d bytes, want %d", ErrTree, tree.GetSize(), fl.info.Size)
	case !bytes.Equal(tree.GetTree(), fl.tree):
		return fmt.Errorf("%w: tree differs from the stored one", ErrTree)
	}
	return nil
}

// store verifies one chunk and writes it to the store.
func (fl *fetchState) store(ctx context.Context, index int, chunk *wirev1.BlobChunk) error {
	data := chunk.GetData()
	if len(data) != chunkLen(fl.info.Size, index) {
		return fmt.Errorf("%w: chunk %d is %d bytes", ErrChunk, index, len(data))
	}
	sum := chunkHash(data)
	if !bytes.Equal(sum[:], chunk.GetChunkHash()) {
		return fmt.Errorf("%w: chunk %d hash", ErrChunk, index)
	}

	err := fl.fetcher.blobs.WriteChunk(ctx, fl.request.Hash, index, data)
	if errors.Is(err, blob.ErrComplete) {
		return nil
	}
	return err
}

// countRange records one range request.
func (fl *fetchState) countRange() {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	fl.ranges++
}

// report records a stored chunk and tells the caller about it.
func (fl *fetchState) report(src source, index int) {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	fl.received++
	fl.bytes += int64(chunkLen(fl.info.Size, index))
	fl.markUsed(src.id)
	if fl.request.Progress == nil {
		return
	}
	fl.request.Progress(Progress{
		Hash:     fl.request.Hash,
		Chunks:   fl.info.Chunks,
		Received: fl.base + fl.received,
		Bytes:    fl.bytes,
		Source:   src.id,
		Index:    index,
	})
}

// markUsed records the first use of a source.
func (fl *fetchState) markUsed(id model.DeviceID) {
	for _, used := range fl.used {
		if used == id {
			return
		}
	}
	fl.used = append(fl.used, id)
}

// drop removes a source from the fetch.
func (fl *fetchState) drop(src source, err error) error {
	fl.sources.drop(src.id)
	fl.fetcher.log.Warn("blob source dropped", "peer", src.id, "hash", hashHex(fl.request.Hash), "error", err)
	return err
}

// result returns what the fetch did.
func (fl *fetchState) result(info blob.Info) Result {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	return Result{
		Info:     info,
		Sources:  append([]model.DeviceID(nil), fl.used...),
		Excluded: fl.sources.failures(),
		Ranges:   fl.ranges,
		Bytes:    fl.bytes,
	}
}

// uniquePeers drops duplicates and peers above the source limit.
func uniquePeers(peers []Peer) []Peer {
	seen := make(map[model.DeviceID]struct{}, len(peers))
	out := make([]Peer, 0, len(peers))
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		id := peer.DeviceID()
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, peer)
		if len(out) == MaxSources {
			break
		}
	}
	return out
}
