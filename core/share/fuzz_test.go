package share

import (
	"context"
	"log/slog"
	"testing"

	"google.golang.org/protobuf/proto"
	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzShareOffer checks that an arbitrary offer is either refused or carries
// files the receiver can store.
func FuzzShareOffer(f *testing.F) {
	sum := blake3.Sum256([]byte("file content"))
	seed := &wirev1.ShareOffer{
		TransferId: "0d1f0e3c",
		Files: []*wirev1.FileMeta{{
			Name:        "report.pdf",
			Size:        12,
			Blake3:      sum[:],
			MtimeUnixMs: 1763000000000,
			MimeType:    "application/pdf",
		}},
	}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x01, 0x61})

	f.Fuzz(func(t *testing.T, data []byte) {
		offer := &wirev1.ShareOffer{}
		if err := proto.Unmarshal(data, offer); err != nil {
			return
		}
		if _, err := checkID(offer.GetTransferId()); err != nil {
			return
		}
		files, err := filesOf(offer.GetFiles())
		if err != nil {
			return
		}
		if len(files) == 0 || len(files) > MaxFiles {
			t.Fatalf("accepted %d files", len(files))
		}
		for _, file := range files {
			if file.Name == "" {
				t.Fatalf("accepted a file without a name")
			}
			if file.Hash == ([32]byte{}) {
				t.Fatalf("accepted a file without a hash")
			}
			if file.Size < 0 || file.Size > 1<<62 {
				t.Fatalf("accepted a file of %d bytes", file.Size)
			}
		}
	})
}

// FuzzShareAnswer checks that a decision, a cancellation or a result of an
// arbitrary peer never disturbs a transfer the service does not know.
func FuzzShareAnswer(f *testing.F) {
	seed := []*wirev1.ControlMessage{
		{Body: &wirev1.ControlMessage_ShareDecision{ShareDecision: &wirev1.ShareDecision{TransferId: "0d1f0e3c", Accepted: true}}},
		{Body: &wirev1.ControlMessage_ShareCancel{ShareCancel: &wirev1.ShareCancel{TransferId: "0d1f0e3c"}}},
		{Body: &wirev1.ControlMessage_ShareResult{ShareResult: &wirev1.ShareResult{TransferId: "0d1f0e3c", Ok: true}}},
	}
	for _, message := range seed {
		if data, err := proto.Marshal(message); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		message := &wirev1.ControlMessage{}
		if err := proto.Unmarshal(data, message); err != nil {
			return
		}

		service := &Service{
			log:     slog.New(slog.DiscardHandler),
			records: make(map[string]*record),
		}
		ctx := context.Background()
		peer := model.DeviceID{}
		peer[0] = 1

		switch body := message.GetBody().(type) {
		case *wirev1.ControlMessage_ShareDecision:
			if err := service.handleDecision(ctx, peer, body.ShareDecision); err != nil {
				return
			}
		case *wirev1.ControlMessage_ShareCancel:
			if err := service.handleCancel(ctx, peer, body.ShareCancel); err != nil {
				return
			}
		case *wirev1.ControlMessage_ShareResult:
			if err := service.handleResult(ctx, peer, body.ShareResult); err != nil {
				return
			}
		}
		if len(service.records) != 0 {
			t.Fatalf("a transfer was created for an unknown offer")
		}
	})
}
