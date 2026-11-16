package transfer

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzBlobRequest checks that a request of an arbitrary peer is either refused
// or asks for a range inside the limits.
func FuzzBlobRequest(f *testing.F) {
	hash := make([]byte, 32)
	hash[0] = 1
	header, err := proto.Marshal(&wirev1.StreamHeader{PayloadType: PayloadRequest, TransferId: "0d1f0e3c"})
	if err != nil {
		f.Fatalf("build a stream header: %v", err)
	}
	request, err := proto.Marshal(&wirev1.BlobRequest{Hash: hash, FromChunk: 0, ToChunk: 8})
	if err != nil {
		f.Fatalf("build a request: %v", err)
	}
	treeRequest, err := proto.Marshal(&wirev1.BlobRequest{Hash: hash, TreeOnly: true})
	if err != nil {
		f.Fatalf("build a tree request: %v", err)
	}
	f.Add(header, request)
	f.Add(header, treeRequest)
	f.Add([]byte{}, []byte{})
	f.Add([]byte{}, []byte{0x0a, 0x20})

	f.Fuzz(func(t *testing.T, headerData, requestData []byte) {
		streamHeader := &wirev1.StreamHeader{}
		if err := proto.Unmarshal(headerData, streamHeader); err != nil {
			return
		}
		message := &wirev1.BlobRequest{}
		if err := proto.Unmarshal(requestData, message); err != nil {
			return
		}
		req, err := parseRequest(streamHeader, message)
		if err != nil {
			return
		}
		if req.hash == ([32]byte{}) {
			t.Fatalf("accepted a zero hash")
		}
		if req.treeOnly {
			return
		}
		if req.from < 0 || req.to < req.from || req.to-req.from >= MaxRangeChunks {
			t.Fatalf("accepted the range %d..%d", req.from, req.to)
		}
		from, to, err := req.rangeOf(req.to + 1)
		if err != nil {
			t.Fatalf("the range %d..%d does not fit a blob of %d chunks", req.from, req.to, req.to+1)
		}
		if from != req.from || to != req.to {
			t.Fatalf("the range %d..%d became %d..%d", req.from, req.to, from, to)
		}
	})
}

// FuzzBlobTree checks that an arbitrary hash tree is either refused or
// describes the blob of its own hash.
func FuzzBlobTree(f *testing.F) {
	tree := &wirev1.BlobTree{Hash: make([]byte, 32)}
	tree.Hash[0] = 1
	tree.Chunks = 1
	if encoded, err := proto.Marshal(tree); err == nil {
		f.Add(encoded)
	}
	f.Add([]byte{})
	f.Add([]byte{0x12, 0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		message := &wirev1.BlobTree{}
		if err := proto.Unmarshal(data, message); err != nil {
			return
		}
		var hash [32]byte
		copy(hash[:], message.GetHash())
		size, tree, err := checkTree(message, hash)
		if err != nil {
			return
		}
		if size < 0 {
			t.Fatalf("accepted a blob of %d bytes", size)
		}
		if int64(len(tree)) != treeSize(chunksFor(size)) {
			t.Fatalf("accepted a tree of %d bytes for %d bytes", len(tree), size)
		}
		if int64(len(tree)) > MaxTreeBytes {
			t.Fatalf("accepted a tree of %d bytes", len(tree))
		}
	})
}

// FuzzHaveBlob checks that an availability message of an arbitrary peer is
// either refused or applied to the fetcher.
func FuzzHaveBlob(f *testing.F) {
	hash := make([]byte, 32)
	hash[0] = 1
	query, err := proto.Marshal(&wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_HaveBlob{HaveBlob: &wirev1.HaveBlob{Hash: hash}},
	})
	if err != nil {
		f.Fatalf("build a query: %v", err)
	}
	answer, err := proto.Marshal(&wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_HaveBlobResp{HaveBlobResp: &wirev1.HaveBlobResp{Hash: hash, Have: true, Chunks: 1}},
	})
	if err != nil {
		f.Fatalf("build an answer: %v", err)
	}
	f.Add(query)
	f.Add(answer)
	f.Add([]byte{})
	f.Add([]byte{0x32, 0x00})

	f.Fuzz(func(_ *testing.T, data []byte) {
		message := &wirev1.ControlMessage{}
		if err := proto.Unmarshal(data, message); err != nil {
			return
		}
		peer := model.DeviceID{}
		peer[0] = 1

		switch body := message.GetBody().(type) {
		case *wirev1.ControlMessage_HaveBlob:
			if len(body.HaveBlob.GetHash()) != 32 {
				return
			}
		case *wirev1.ControlMessage_HaveBlobResp:
			fetcher := &Fetcher{}
			fetcher.answer(peer, body.HaveBlobResp)
		}
	})
}
