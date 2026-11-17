package node

import (
	"context"
	"testing"

	"github.com/lebe-dev/unison/core/discovery"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// untaggedDiscovery reports one address of a device without the group tag, the
// way a device that has no discovery key announces itself.
type untaggedDiscovery struct {
	*fake.Discovery
	addr string
}

var _ taggedDiscovery = (*untaggedDiscovery)(nil)

// BrowseCandidates delivers the one address without a tag.
func (d *untaggedDiscovery) BrowseCandidates(ctx context.Context, service ports.Service) (<-chan discovery.Candidate, error) {
	candidates := make(chan discovery.Candidate, 1)
	if err := ctx.Err(); err != nil {
		close(candidates)
		return candidates, err
	}
	if service == ports.ServiceMain {
		candidates <- discovery.Candidate{Addr: d.addr, TXT: map[string]string{"v": "1"}}
	}
	close(candidates)
	return candidates, nil
}

// TestDialsUntaggedCandidateForOfflinePeer checks that a paired device that
// announces without the group tag is dialed while it is offline.
func TestDialsUntaggedCandidateForOfflinePeer(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	a := newTestNode(t, network, fake.NewBus(), 43000)
	a.start(t)

	disc := &untaggedDiscovery{Discovery: fake.NewBus().Discovery(), addr: a.listenAddr()}
	b := newTestNodeWith(t, network, fake.NewBus(), 43100, t.TempDir(), fake.NewKeystore(), func(cfg *Config) {
		cfg.Discovery = disc
	})
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)

	waitOnline(t, b, a.DeviceID())
	waitOnline(t, a, b.DeviceID())
}
