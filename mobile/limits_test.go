package mobile

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

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
