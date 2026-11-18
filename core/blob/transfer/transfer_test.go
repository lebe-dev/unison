package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
)

// testTimeout bounds one integration test.
const testTimeout = 60 * time.Second

// shareAllow allows the blobs of the listed shares.
type shareAllow map[string]bool

// AllowBlob reports whether the share is listed.
func (s shareAllow) AllowBlob(_ context.Context, _ model.DeviceID, transferID string, _ [32]byte) bool {
	return s[transferID]
}

func TestFetchFromOneSource(t *testing.T) {
	h := newTestNetwork(t)
	src := h.newNode(1)
	empty := h.newNode(2)
	fl := h.newNode(3)

	data := randomBytes(t, 3*blob.ChunkSize+123)
	info := put(t, src.blobs, data)

	h.connect(fl, src)
	h.connect(fl, empty)

	res, err := fetch(t, fl, info.Hash, src, empty)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.Info.Chunks != 4 || res.Info.Size != int64(len(data)) {
		t.Fatalf("info is %d chunks of %d bytes, want 4 of %d", res.Info.Chunks, res.Info.Size, len(data))
	}
	if len(res.Excluded) != 0 {
		t.Fatalf("excluded %v, want none", res.Excluded)
	}
	if want := []model.DeviceID{src.DeviceID()}; !slices.Equal(res.Sources, want) {
		t.Fatalf("sources %v, want %v", res.Sources, want)
	}
	if got := content(t, fl.blobs, info.Hash); !bytes.Equal(got, data) {
		t.Fatalf("content of %d bytes differs from the source", len(got))
	}
}

func TestFetchFromTwoSources(t *testing.T) {
	h := newTestNetwork(t)
	first := h.newNode(1)
	second := h.newNode(2)
	fl := h.newNode(3)

	data := randomBytes(t, 4*blob.ChunkSize)
	info := put(t, first.blobs, data)
	put(t, second.blobs, data)

	h.connect(fl, first)
	h.connect(fl, second)

	res, err := fetch(t, fl, info.Hash, first, second)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(res.Sources) != 2 {
		t.Fatalf("sources %v, want both peers", res.Sources)
	}
	if got := content(t, fl.blobs, info.Hash); !bytes.Equal(got, data) {
		t.Fatalf("content of %d bytes differs from the source", len(got))
	}
}

func TestFetchResumesAfterInterruption(t *testing.T) {
	h := newTestNetwork(t)
	src := h.newNode(1)
	fl := h.newNode(2)

	data := randomBytes(t, 4*blob.ChunkSize)
	info := put(t, src.blobs, data)

	h.connect(fl, src)

	// The source disappears after the first stored chunk.
	var once sync.Once
	interrupt := func(Progress) {
		once.Do(func() { src.closeSession(fl.DeviceID()) })
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := fl.fetch.Fetch(ctx, FetchRequest{
		Hash:     info.Hash,
		Peers:    []Peer{fl.sessionTo(src)},
		Progress: interrupt,
	}); err == nil {
		t.Fatal("fetch of an interrupted source reported success")
	}

	stored, err := fl.blobs.Stat(ctx, info.Hash)
	if err != nil {
		t.Fatalf("stat after the interruption: %v", err)
	}
	if !stored.Partial {
		t.Fatal("interrupted blob is complete")
	}
	missing, err := fl.blobs.Missing(ctx, info.Hash)
	if err != nil {
		t.Fatalf("missing chunks: %v", err)
	}
	if len(missing) == 0 || len(missing) >= info.Chunks {
		t.Fatalf("%d of %d chunks missing after the interruption", len(missing), info.Chunks)
	}

	h.connect(fl, src)

	var got, counts []int
	res, err := fl.fetch.Fetch(ctx, FetchRequest{
		Hash:  info.Hash,
		Peers: []Peer{fl.sessionTo(src)},
		Progress: func(p Progress) {
			got = append(got, p.Index)
			counts = append(counts, p.Received)
		},
	})
	if err != nil {
		t.Fatalf("resumed fetch: %v", err)
	}
	if res.Info.Partial {
		t.Fatal("resumed blob is partial")
	}
	for _, index := range got {
		if slices.Contains(missing, index) {
			continue
		}
		t.Fatalf("chunk %d was fetched again, missing chunks were %v", index, missing)
	}
	if len(counts) != len(missing) {
		t.Fatalf("%d progress reports for %d missing chunks", len(counts), len(missing))
	}
	for i, count := range counts {
		if want := info.Chunks - len(missing) + i + 1; count != want {
			t.Fatalf("progress %d reports %d chunks, want %d", i, count, want)
		}
	}
	if got := content(t, fl.blobs, info.Hash); !bytes.Equal(got, data) {
		t.Fatalf("content of %d bytes differs from the source", len(got))
	}
}

func TestFetchDropsCorruptSource(t *testing.T) {
	tests := []struct {
		name string
		from int
	}{
		{name: "first chunk", from: 0},
		{name: "later chunk", from: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newTestNetwork(t)
			bad := h.newNode(1)
			good := h.newNode(2)
			fl := h.newNode(3)

			data := randomBytes(t, 3*blob.ChunkSize)
			info := put(t, bad.blobs, data)
			put(t, good.blobs, data)

			bad.setHandler(func(model.DeviceID) session.RequestHandler {
				return corruptHandler(t, bad.blobs, info.Hash, test.from)
			})

			h.connect(fl, bad)
			h.connect(fl, good)

			res, err := fetch(t, fl, info.Hash, bad, good)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if want := []model.DeviceID{bad.DeviceID()}; !slices.Equal(res.Excluded, want) {
				t.Fatalf("excluded %v, want %v", res.Excluded, want)
			}
			if !slices.Contains(res.Sources, good.DeviceID()) {
				t.Fatalf("sources %v, want %v", res.Sources, good.DeviceID())
			}
			if got := content(t, fl.blobs, info.Hash); !bytes.Equal(got, data) {
				t.Fatalf("content of %d bytes differs from the source", len(got))
			}
		})
	}
}

func TestFetchRestartsUnprovenPartial(t *testing.T) {
	h := newTestNetwork(t)
	src := h.newNode(1)
	fl := h.newNode(2)

	data := randomBytes(t, 2*blob.ChunkSize)
	info := put(t, src.blobs, data)

	// A partial blob without chunks may hold a tree of another content, which
	// no chunk can prove yet.
	ctx := context.Background()
	tree, err := src.blobs.Tree(ctx, info.Hash)
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	wrong := slices.Clone(tree)
	wrong[len(wrong)-1] ^= 0xff
	if _, err := fl.blobs.CreatePartial(ctx, info.Hash, info.Size, wrong); err != nil {
		t.Fatalf("start a partial with a wrong tree: %v", err)
	}

	h.connect(fl, src)

	if _, err := fetch(t, fl, info.Hash, src); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := content(t, fl.blobs, info.Hash); !bytes.Equal(got, data) {
		t.Fatalf("content of %d bytes differs from the source", len(got))
	}
}

func TestFetchNoSource(t *testing.T) {
	h := newTestNetwork(t)
	first := h.newNode(1)
	second := h.newNode(2)
	fl := h.newNode(3)

	data := randomBytes(t, blob.ChunkSize)
	info := put(t, second.blobs, data)
	if err := second.blobs.Remove(context.Background(), info.Hash); err != nil {
		t.Fatalf("remove %s: %v", hashHex(info.Hash), err)
	}

	h.connect(fl, first)
	h.connect(fl, second)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	_, err := fl.fetch.Fetch(ctx, FetchRequest{
		Hash:  info.Hash,
		Peers: []Peer{fl.sessionTo(first), fl.sessionTo(second)},
	})
	if !errors.Is(err, ErrNoSource) {
		t.Fatalf("fetch error is %v, want %v", err, ErrNoSource)
	}
}

func TestFetchFriendsDenied(t *testing.T) {
	h := newTestNetwork(t)
	src := h.newNode(1)
	fl := h.newNode(2)

	data := randomBytes(t, blob.ChunkSize)
	info := put(t, src.blobs, data)

	// Friend devices may not fetch blobs of the history.
	h.connectAs(fl, src, model.RoleFriends, model.RoleFriends)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	_, err := fl.fetch.Fetch(ctx, FetchRequest{Hash: info.Hash, Peers: []Peer{fl.sessionTo(src)}})
	if !errors.Is(err, ErrNoSource) {
		t.Fatalf("fetch error is %v, want %v", err, ErrNoSource)
	}
}

func TestFetchShareByTransferID(t *testing.T) {
	h := newTestNetwork(t)
	src := h.newNodeWith(1, shareAllow{"share-1": true})
	fl := h.newNode(2)

	data := randomBytes(t, blob.ChunkSize)
	info := put(t, src.blobs, data)

	h.connect(fl, src)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := fl.fetch.Fetch(ctx, FetchRequest{
		Hash:       info.Hash,
		Peers:      []Peer{fl.sessionTo(src)},
		TransferID: "share-2",
	}); !errors.Is(err, ErrDenied) {
		t.Fatalf("fetch of a foreign share: error is %v, want %v", err, ErrDenied)
	}
	if has, err := fl.blobs.Has(ctx, info.Hash); err != nil || has {
		t.Fatalf("foreign share left a stored blob: %v, %v", has, err)
	}

	if _, err := fl.fetch.Fetch(ctx, FetchRequest{
		Hash:       info.Hash,
		Peers:      []Peer{fl.sessionTo(src)},
		TransferID: "share-1",
	}); err != nil {
		t.Fatalf("fetch of the offered share: %v", err)
	}
	if got := content(t, fl.blobs, info.Hash); !bytes.Equal(got, data) {
		t.Fatalf("content of %d bytes differs from the source", len(got))
	}
}

func TestFetchEmptyBlob(t *testing.T) {
	h := newTestNetwork(t)
	src := h.newNode(1)
	fl := h.newNode(2)

	info := put(t, src.blobs, nil)

	h.connect(fl, src)

	res, err := fetch(t, fl, info.Hash, src)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.Info.Size != 0 || res.Info.Chunks != 0 {
		t.Fatalf("info is %d chunks of %d bytes, want empty", res.Info.Chunks, res.Info.Size)
	}
}

// fetch downloads a blob from the given nodes.
func fetch(t *testing.T, fl *node, hash [32]byte, sources ...*node) (Result, error) {
	t.Helper()
	peers := make([]Peer, 0, len(sources))
	for _, src := range sources {
		peers = append(peers, fl.sessionTo(src))
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	return fl.fetch.Fetch(ctx, FetchRequest{Hash: hash, Peers: peers})
}

// randomBytes returns n random bytes.
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	return data
}

// TestFetchRefusesBlobAboveMaxSize checks that a blob larger than the size the
// peer announced is not downloaded, and that one within it is.
func TestFetchRefusesBlobAboveMaxSize(t *testing.T) {
	h := newTestNetwork(t)
	src := h.newNode(1)
	fl := h.newNode(2)

	data := randomBytes(t, 2*blob.ChunkSize+1)
	info := put(t, src.blobs, data)
	h.connect(fl, src)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	_, err := fl.fetch.Fetch(ctx, FetchRequest{
		Hash:    info.Hash,
		Peers:   []Peer{fl.sessionTo(src)},
		MaxSize: int64(len(data)) - 1,
	})
	if !errors.Is(err, ErrSize) {
		t.Fatalf("fetch error is %v, want %v", err, ErrSize)
	}
	if has, err := fl.blobs.Has(ctx, info.Hash); err != nil || has {
		t.Fatalf("the refused blob is stored (%v)", err)
	}
	if stat, err := fl.blobs.Stat(ctx, info.Hash); err == nil {
		t.Fatalf("the refused blob left a partial of %d bytes", stat.Size)
	}

	res, err := fl.fetch.Fetch(ctx, FetchRequest{
		Hash:    info.Hash,
		Peers:   []Peer{fl.sessionTo(src)},
		MaxSize: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("fetch within the size: %v", err)
	}
	if res.Info.Size != int64(len(data)) {
		t.Fatalf("fetched %d bytes, want %d", res.Info.Size, len(data))
	}
}
