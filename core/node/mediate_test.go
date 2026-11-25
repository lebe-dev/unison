package node

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/mediate"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/relay"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// waitMediatedOffer waits until the node holds an incoming exchange with the
// mediator.
func waitMediatedOffer(t *testing.T, n *testNode, mediator model.DeviceID) mediate.Exchange {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		for _, exchange := range n.MediatedPairs() {
			if exchange.Direction == mediate.DirectionIncoming && exchange.Mediator.ID == mediator {
				return exchange
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s received no offer from %s", n.DeviceID(), mediator)
	return mediate.Exchange{}
}

// TestMediatedPairOverRelay checks a hybrid network: the mediator and one
// endpoint share the local network, the other endpoint reaches the mediator and
// the new peer through the relay only.
func TestMediatedPairOverRelay(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()
	server := startNodeTestRelay(t)

	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}
	a := newRelayNode(t, network, bus, 51000, Config{Relay: trusted})
	b := newRelayNode(t, network, bus, 51004, Config{Relay: trusted})
	c := newRelayNode(t, network, bus, 51008, Config{Relay: trusted})
	for _, n := range []*testNode{a, b, c} {
		n.start(t)
	}

	setRelay(t, a, server.url)
	setRelay(t, b, server.url)
	setRelay(t, c, server.url)

	a.pair(t, b)
	b.pair(t, a)
	waitPath(t, a, b.DeviceID(), model.PathLAN)

	// The third device is in another network, so it reaches the mediator over
	// the relay only.
	c.sockets.disconnect()
	b.pair(t, c)
	c.pair(t, b)
	waitPath(t, b, c.DeviceID(), model.PathRelay)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := b.StartMediatedPair(ctx, a.DeviceID(), c.DeviceID(), model.RolePersonal); err != nil {
		t.Fatalf("start the exchange: %v", err)
	}
	offerA := waitMediatedOffer(t, a, b.DeviceID())
	offerC := waitMediatedOffer(t, c, b.DeviceID())
	if offerA.SAS.String() != offerC.SAS.String() {
		t.Fatalf("the endpoints show different verification codes")
	}
	if err := a.ConfirmMediatedPair(ctx, offerA.SessionID, false); err != nil {
		t.Fatalf("alpha confirms: %v", err)
	}
	if err := c.ConfirmMediatedPair(ctx, offerC.SessionID, false); err != nil {
		t.Fatalf("gamma confirms: %v", err)
	}

	waitTrusted(t, a, c.DeviceID())
	waitTrusted(t, c, a.DeviceID())
	waitPath(t, a, c.DeviceID(), model.PathRelay)
	waitPath(t, c, a.DeviceID(), model.PathRelay)
}
