package node

import (
	"context"
	"testing"

	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// TestRetryPeerOutlivesRequest checks that the dials of a retry keep going after
// the request that asked for it ends, as an API call does once it answers.
func TestRetryPeerOutlivesRequest(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	// The nodes share no discovery and store no address of each other, so the
	// cached candidate is the only way to reach the peer.
	a := newTestNode(t, network, fake.NewBus(), 43200)
	b := newTestNode(t, network, fake.NewBus(), 43300)
	a.start(t)
	b.start(t)
	a.pair(t, b)
	b.pair(t, a)
	a.rememberCandidate(b.listenAddr())

	ctx, cancel := context.WithCancel(context.Background())
	if err := a.RetryPeer(ctx, b.DeviceID()); err != nil {
		cancel()
		t.Fatalf("retry the peer: %v", err)
	}
	cancel()

	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())
}
