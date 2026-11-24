package mobile

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// TestEncodeResponseLimits checks that the default cap stays at 1 MiB and that
// only the thumbnail response may grow to 8 MiB.
func TestEncodeResponseLimits(t *testing.T) {
	if maxThumbnailBytes != 8<<20 {
		t.Fatalf("thumbnail cap = %d, want 8 MiB", maxThumbnailBytes)
	}
	if maxThumbnailBytes < 1024*1024*4 {
		t.Fatalf("thumbnail cap %d does not hold a 1024 px image", maxThumbnailBytes)
	}

	large := &localv1.GetHistoryThumbnailResponse{Rgba: make([]byte, maxCallBytes+1)}
	if code := connect.Code(encodeResponse(large, maxCallBytes).GetCode()); code != connect.CodeResourceExhausted {
		t.Fatalf("a large response returned code %v, want ResourceExhausted", code)
	}
	response := encodeResponse(large, maxThumbnailBytes)
	if response.GetCode() != 0 || len(response.GetPayload()) == 0 {
		t.Fatalf("a large thumbnail returned code %d", response.GetCode())
	}
}

// TestConfigRetriesOfflinePeersRarely checks that the phone does not dial a
// peer that is away every thirty seconds: discovery, a network change and the
// peer itself bring it back sooner.
func TestConfigRetriesOfflinePeersRarely(t *testing.T) {
	data, err := proto.Marshal(&mobilev1.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cfg, err := parseConfig(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.limits.OfflineRetryMaxDelay != 5*time.Minute {
		t.Fatalf("offline retry cap = %v, want 5m", cfg.limits.OfflineRetryMaxDelay)
	}
}
