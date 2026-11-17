package node

import (
	"context"
	"slices"
	"testing"
	"time"

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

// BrowseCandidates delivers the one address without a tag again and again, as
// mDNS does with every answer to its periodic query.
func (d *untaggedDiscovery) BrowseCandidates(ctx context.Context, service ports.Service) (<-chan discovery.Candidate, error) {
	candidates := make(chan discovery.Candidate, 1)
	if err := ctx.Err(); err != nil {
		close(candidates)
		return candidates, err
	}
	if service != ports.ServiceMain {
		close(candidates)
		return candidates, nil
	}
	go func() {
		defer close(candidates)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case candidates <- discovery.Candidate{Addr: d.addr, TXT: map[string]string{"v": "1"}}:
			case <-ctx.Done():
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
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
	if !b.cachesCandidate(disc.addr) {
		t.Fatal("the untagged address is not kept for the next reconnect")
	}
}

// feedDiscovery delivers the candidates the test sends, one at a time, and
// announces through the bus it embeds.
type feedDiscovery struct {
	*fake.Discovery
	feed chan discovery.Candidate
}

var _ taggedDiscovery = (*feedDiscovery)(nil)

// BrowseCandidates delivers the candidates of the feed for the main service.
func (d *feedDiscovery) BrowseCandidates(_ context.Context, service ports.Service) (<-chan discovery.Candidate, error) {
	if service != ports.ServiceMain {
		empty := make(chan discovery.Candidate)
		close(empty)
		return empty, nil
	}
	return d.feed, nil
}

// cachesCandidate reports whether the node keeps addr for the next reconnect.
func (n *testNode) cachesCandidate(addr string) bool {
	return slices.Contains(n.candidateAddrs(), addr)
}

// TestIgnoresUntaggedCandidateWhilePeersOnline checks that a device without the
// group tag is neither dialed nor kept while every paired device is online: on
// a network with devices of other groups, every reconnect would dial them.
func TestIgnoresUntaggedCandidateWhilePeersOnline(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	disc := &feedDiscovery{Discovery: bus.Discovery(), feed: make(chan discovery.Candidate)}
	a := newTestNodeWith(t, network, bus, 43400, t.TempDir(), fake.NewKeystore(), func(cfg *Config) {
		cfg.Discovery = disc
	})
	b := newTestNode(t, network, bus, 43500)
	stranger := newTestNode(t, network, fake.NewBus(), 43600)
	a.start(t)
	b.start(t)
	stranger.start(t)
	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	untagged := stranger.listenAddr()
	tagged := b.listenAddr()
	send := func(candidate discovery.Candidate) {
		t.Helper()
		select {
		case disc.feed <- candidate:
		case <-time.After(testTimeout):
			t.Fatal("the node did not take the candidate")
		}
	}
	send(discovery.Candidate{Addr: untagged})
	// The feed is unbuffered, so the node handled the first candidate once it
	// takes the second one.
	send(discovery.Candidate{Addr: tagged, Matched: true})

	if a.cachesCandidate(untagged) {
		t.Fatal("an untagged device is kept while every paired device is online")
	}
	deadline := time.Now().Add(testTimeout)
	for !a.cachesCandidate(tagged) {
		if time.Now().After(deadline) {
			t.Fatal("a device with the group tag is not kept")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
