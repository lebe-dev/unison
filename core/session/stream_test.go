package session

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// blobChunk returns a message that carries one chunk of payload.
func blobChunk(index uint32, size int) *wirev1.BlobChunk {
	return &wirev1.BlobChunk{Index: index, Data: make([]byte, size)}
}

// TestRequestStream checks a request with its answer on a bidirectional stream.
func TestRequestStream(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	served := make(chan string, 1)

	client, _ := h.open(func(_, server *Session) {
		err := server.HandleRequest("test.request", trust.ActionFetchBlob, func(_ context.Context, req *Request) error {
			served <- req.Header().GetTransferId()

			query := &wirev1.BlobRequest{}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := req.Read(ctx, query); err != nil {
				return err
			}
			return req.Write(ctx, &wirev1.HaveBlobResp{Have: true, Chunks: 4})
		})
		if err != nil {
			t.Fatalf("handle request: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := client.OpenRequest(ctx, &wirev1.StreamHeader{PayloadType: "test.request", TransferId: "t-1"})
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	if got := req.Header().GetKind(); got != wirev1.StreamKind_STREAM_KIND_REQUEST {
		t.Fatalf("header kind = %s, want request", got)
	}
	if err := req.Write(ctx, &wirev1.BlobRequest{FromChunk: 2}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	answer := &wirev1.HaveBlobResp{}
	if err := req.Read(ctx, answer); err != nil {
		t.Fatalf("read answer: %v", err)
	}
	if !answer.GetHave() || answer.GetChunks() != 4 {
		t.Fatalf("answer = %v, want 4 chunks", answer)
	}
	_ = req.Close()

	select {
	case id := <-served:
		if id != "t-1" {
			t.Fatalf("transfer id = %q, want t-1", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request did not reach the handler")
	}
}

// TestRequestStreamDenied checks that a request stream of an action the role
// forbids never reaches its handler.
func TestRequestStreamDenied(t *testing.T) {
	h := newHarness(t, model.RoleFriends, model.RoleFriends)
	var handled bool
	var mu sync.Mutex

	client, server := h.open(func(_, server *Session) {
		err := server.HandleRequest("test.request", trust.ActionFetchBlob, func(context.Context, *Request) error {
			mu.Lock()
			handled = true
			mu.Unlock()
			return nil
		})
		if err != nil {
			t.Fatalf("handle request: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := client.OpenRequest(ctx, &wirev1.StreamHeader{PayloadType: "test.request"})
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = req.Close() }()

	deadline := time.Now().Add(5 * time.Second)
	for server.Violations() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the request was not denied")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if handled {
		t.Fatal("the handler of a denied request ran")
	}
}

// TestTransferStream checks a unidirectional transfer.
func TestTransferStream(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	type received struct {
		transferID string
		chunks     []uint32
	}
	done := make(chan received, 1)

	client, _ := h.open(func(_, server *Session) {
		err := server.HandleTransfer("test.transfer", trust.ActionSyncHistory, func(_ context.Context, transfer *Transfer) error {
			got := received{transferID: transfer.Header().GetTransferId()}
			for {
				chunk := &wirev1.BlobChunk{}
				err := transfer.Read(context.Background(), chunk)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return err
				}
				got.chunks = append(got.chunks, chunk.GetIndex())
			}
			done <- got
			return nil
		})
		if err != nil {
			t.Fatalf("handle transfer: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	writer, err := client.OpenTransfer(ctx, &wirev1.StreamHeader{PayloadType: "test.transfer", TransferId: "t-2"})
	if err != nil {
		t.Fatalf("open transfer: %v", err)
	}
	for i := range 3 {
		if err := writer.Write(ctx, blobChunk(uint32(i), 16)); err != nil {
			t.Fatalf("write chunk %d: %v", i, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close transfer: %v", err)
	}

	select {
	case got := <-done:
		if got.transferID != "t-2" {
			t.Fatalf("transfer id = %q, want t-2", got.transferID)
		}
		if len(got.chunks) != 3 {
			t.Fatalf("chunks = %v, want 3", got.chunks)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the transfer did not reach the handler")
	}
}

// TestStreamWithoutHandler checks that a stream of an unknown payload type is
// dropped without ending the session.
func TestStreamWithoutHandler(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	client, server := h.open(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := client.OpenRequest(ctx, &wirev1.StreamHeader{PayloadType: "unknown"})
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = req.Close() }()

	// The session drops the stream and closes its side.
	if err := req.Read(ctx, &wirev1.HaveBlobResp{}); err == nil {
		t.Fatal("the request without a handler was answered")
	}
	if server.Violations() != 0 {
		t.Fatal("a stream without a handler was counted as a violation")
	}
	if server.isClosed() {
		t.Fatal("a stream without a handler ended the session")
	}
}

// TestStreamHeaderRefused checks the header validation of an incoming stream.
func TestStreamHeaderRefused(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	server, closed := h.rawOpen(&wirev1.Hello{ProtocolVersion: ProtocolVersion, Name: "client"})
	if err := <-closed; err != nil {
		t.Fatalf("open: %v", err)
	}

	stream, err := h.clientConn.OpenStream(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	// A transfer header on a bidirectional stream.
	if err := writeFrame(stream, &wirev1.StreamHeader{
		Kind:        wirev1.StreamKind_STREAM_KIND_TRANSFER,
		PayloadType: "test.transfer",
	}, MaxStreamMessage); err != nil {
		t.Fatalf("write header: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for server.Violations() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the wrong stream kind was not counted as a violation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRequestStreamLimit checks that a peer that keeps too many request streams
// open is refused.
func TestRequestStreamLimit(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	started := make(chan struct{}, MaxRequestStreams)
	release := make(chan struct{})

	client, server := h.open(func(_, server *Session) {
		err := server.HandleRequest("test.request", trust.ActionFetchBlob, func(ctx context.Context, _ *Request) error {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		})
		if err != nil {
			t.Fatalf("handle request: %v", err)
		}
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requests := make([]*Request, 0, MaxRequestStreams)
	for range MaxRequestStreams {
		req, err := client.OpenRequest(ctx, &wirev1.StreamHeader{PayloadType: "test.request"})
		if err != nil {
			t.Fatalf("open request: %v", err)
		}
		requests = append(requests, req)
	}
	for range MaxRequestStreams {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("not every request reached its handler")
		}
	}

	extra, err := client.OpenRequest(ctx, &wirev1.StreamHeader{PayloadType: "test.request"})
	if err != nil {
		t.Fatalf("open request over the limit: %v", err)
	}
	defer func() { _ = extra.Close() }()

	deadline := time.Now().Add(5 * time.Second)
	for server.Violations() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the request over the limit was not counted as a violation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if server.isClosed() {
		t.Fatal("one request over the limit ended the session")
	}
	for _, req := range requests {
		_ = req.Close()
	}
}

// TestOpenStreamRefusedBeforeOpen checks that a stream cannot be opened before
// the Hello exchange.
func TestOpenStreamRefusedBeforeOpen(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	client, err := New(h.clientConn, h.clientCfg)
	if err != nil {
		t.Fatalf("session: %v", err)
	}

	ctx := context.Background()
	if _, err := client.OpenRequest(ctx, &wirev1.StreamHeader{PayloadType: "test"}); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("open request error = %v, want ErrNotOpen", err)
	}
	if _, err := client.OpenTransfer(ctx, &wirev1.StreamHeader{PayloadType: "test"}); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("open transfer error = %v, want ErrNotOpen", err)
	}
	if err := client.Send(ctx, clipAnnounce(1)); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("send error = %v, want ErrNotOpen", err)
	}
}

// TestStreamHeaderValidation checks the header checks of an outgoing stream.
func TestStreamHeaderValidation(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	client, _ := h.open(nil)
	ctx := context.Background()

	if _, err := client.OpenRequest(ctx, nil); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("nil header error = %v, want ErrInvalidHeader", err)
	}
	if _, err := client.OpenRequest(ctx, &wirev1.StreamHeader{}); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("missing payload type error = %v, want ErrInvalidHeader", err)
	}
	wrong := &wirev1.StreamHeader{PayloadType: "test", Kind: wirev1.StreamKind_STREAM_KIND_TRANSFER}
	if _, err := client.OpenRequest(ctx, wrong); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("wrong kind error = %v, want ErrInvalidHeader", err)
	}
	if wrong.GetKind() != wirev1.StreamKind_STREAM_KIND_TRANSFER {
		t.Fatal("the caller header was modified")
	}
}
