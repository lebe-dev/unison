package history

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzHistorySync checks that an arbitrary version vector is either refused or
// fits the limit on the origins it names.
func FuzzHistorySync(f *testing.F) {
	origin := make([]byte, 32)
	origin[0] = 1
	id, err := model.DeviceIDFromBytes(origin)
	if err != nil {
		f.Fatalf("build a device id: %v", err)
	}
	seed := &wirev1.HistorySync{Vector: map[string]uint64{id.String(): 7}}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x03, 0x0a, 0x01, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		message := &wirev1.HistorySync{}
		if err := proto.Unmarshal(data, message); err != nil {
			return
		}
		vector, err := vectorFromProto(message.GetVector())
		if err != nil {
			return
		}
		if len(vector) > maxVectorOrigins {
			t.Fatalf("accepted %d origins", len(vector))
		}
		for origin, seq := range vector {
			if origin.IsZero() || seq == 0 {
				t.Fatalf("accepted the version %s at %d", origin, seq)
			}
		}
	})
}

// FuzzHistoryBatch checks that every entry of an arbitrary batch is either
// refused on its own or passes the checks the store runs before it records it.
func FuzzHistoryBatch(f *testing.F) {
	item := &wirev1.ClipItem{
		Id:              make([]byte, 16),
		Origin:          make([]byte, 32),
		Seq:             1,
		CreatedAtUnixMs: 1763000000000,
		ExpiresAtUnixMs: 1763086400000,
		Kind:            wirev1.Kind_KIND_TEXT,
		Source:          wirev1.Source_SOURCE_CLIPBOARD,
	}
	item.Origin[0] = 1
	seed := &wirev1.HistoryBatch{Items: []*wirev1.ClipItem{item}}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		batch := &wirev1.HistoryBatch{}
		if err := proto.Unmarshal(data, batch); err != nil {
			return
		}
		items := batch.GetItems()
		if len(items) > MaxBatchItems {
			items = items[:MaxBatchItems]
		}
		for _, raw := range items {
			item, err := protoconv.ClipItemFromProto(raw)
			if err != nil {
				continue
			}
			if err := validate(item); err != nil {
				continue
			}
			if item.ID.IsZero() || item.Origin.IsZero() || item.Seq == 0 {
				t.Fatalf("accepted an item without an id, an origin or a sequence")
			}
			if len(item.Representations) > maxRepresentations {
				t.Fatalf("accepted %d representations", len(item.Representations))
			}
		}
	})
}
