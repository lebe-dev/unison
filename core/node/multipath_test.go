package node

import (
	"crypto/tls"
	"slices"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/relay"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// mixedGroup is a group of three devices: phone and mac share a local network,
// remote reaches both only through the relay.
type mixedGroup struct {
	phone, mac, remote *testNode
}

// startMixedGroup starts the three devices, pairs every one with the others and
// waits until each pair runs over its expected path.
func startMixedGroup(t *testing.T, base int) mixedGroup {
	t.Helper()

	network := vnet.NewNetwork()
	bus := fake.NewBus()
	server := startNodeTestRelay(t)

	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}
	g := mixedGroup{
		phone:  newRelayNode(t, network, bus, base, Config{Relay: trusted}),
		mac:    newRelayNode(t, network, bus, base+4, Config{Relay: trusted}),
		remote: newRelayNode(t, network, bus, base+8, Config{Relay: trusted}),
	}
	all := []*testNode{g.phone, g.mac, g.remote}
	for _, n := range all {
		n.start(t)
	}
	// The remote device is in another network: its local sockets carry nothing.
	g.remote.sockets.disconnect()

	for _, n := range all {
		setRelay(t, n, server.url)
	}
	for _, n := range all {
		for _, other := range all {
			if n != other {
				n.pair(t, other)
			}
		}
	}

	waitPath(t, g.phone, g.mac.DeviceID(), model.PathLAN)
	waitPath(t, g.mac, g.phone.DeviceID(), model.PathLAN)
	waitPath(t, g.phone, g.remote.DeviceID(), model.PathRelay)
	waitPath(t, g.remote, g.phone.DeviceID(), model.PathRelay)
	waitPath(t, g.mac, g.remote.DeviceID(), model.PathRelay)
	waitPath(t, g.remote, g.mac.DeviceID(), model.PathRelay)
	settle(t, g.phone, g.mac)
	settle(t, g.phone, g.remote)
	settle(t, g.mac, g.remote)
	return g
}

// TestMixedPathsServeEveryPeer keeps a local network session and relay sessions
// side by side, and a clipboard item reaches every device over its own path.
func TestMixedPathsServeEveryPeer(t *testing.T) {
	g := startMixedGroup(t, 23100)

	lanGen := g.phone.generation(g.mac.DeviceID())

	submitText(t, g.remote, "hello from the remote device")
	waitClipboard(t, g.phone.clipboard, "hello from the remote device")
	waitClipboard(t, g.mac.clipboard, "hello from the remote device")

	submitText(t, g.phone, "hello from the phone")
	waitClipboard(t, g.remote.clipboard, "hello from the phone")
	waitClipboard(t, g.mac.clipboard, "hello from the phone")

	// Looking for the remote device in the local network must not disturb the
	// session with the mac.
	time.Sleep(500 * time.Millisecond)
	if got := g.phone.generation(g.mac.DeviceID()); got != lanGen {
		t.Fatalf("the local network session was replaced while the relay ran: generation %d, want %d", got, lanGen)
	}
	waitPath(t, g.phone, g.mac.DeviceID(), model.PathLAN)
}

// TestRelayPeerSkipsAddressesOfOnlinePeers checks that the search for a relay
// peer in the local network does not dial the address of a device that is
// already online there: the handshake would only find that device again.
func TestRelayPeerSkipsAddressesOfOnlinePeers(t *testing.T) {
	g := startMixedGroup(t, 23120)

	macAddr := g.mac.listenAddr()
	addrs := g.phone.dialAddrs(ctxOf(t), g.remote.DeviceID())
	if slices.Contains(addrs, macAddr) {
		t.Fatalf("addresses for the remote device include the mac at %s: %v", macAddr, addrs)
	}

	// The mac itself is still dialed on its own address.
	if addrs := g.phone.dialAddrs(ctxOf(t), g.mac.DeviceID()); !slices.Contains(addrs, macAddr) {
		t.Fatalf("addresses for the mac miss %s: %v", macAddr, addrs)
	}
}

// TestIntroducedRemoteDeviceUsesTheRelay pairs the remote device with the mac
// only. The phone learns it from the mac, gets the relay access of the group and
// reaches it through the relay, with the mailbox derived from the group token.
func TestIntroducedRemoteDeviceUsesTheRelay(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	server := startNodeTestRelay(t)

	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}
	phone := newRelayNode(t, network, bus, 23160, Config{Relay: trusted})
	mac := newRelayNode(t, network, bus, 23164, Config{Relay: trusted})
	remote := newRelayNode(t, network, bus, 23168, Config{Relay: trusted})
	for _, n := range []*testNode{phone, mac, remote} {
		n.start(t)
	}

	phone.pair(t, mac)
	mac.pair(t, phone)
	waitPath(t, phone, mac.DeviceID(), model.PathLAN)
	setRelay(t, phone, server.url)
	waitGroupRelay(t, mac, trust.RelaySettings{URL: server.url, Token: relayTestToken})

	// The remote device pairs with the mac while it is in the same network,
	// then leaves for another one.
	pairThroughWindow(t, remote, mac)
	waitTrusted(t, phone, remote.DeviceID())
	waitTrusted(t, remote, phone.DeviceID())
	waitGroupRelay(t, remote, trust.RelaySettings{URL: server.url, Token: relayTestToken})
	remote.sockets.disconnect()
	for _, peer := range []*testNode{phone, mac} {
		if l := remote.lookup(peer.DeviceID()); l != nil {
			l.close()
		}
		if l := peer.lookup(remote.DeviceID()); l != nil {
			l.close()
		}
	}

	waitPath(t, phone, remote.DeviceID(), model.PathRelay)
	waitPath(t, remote, phone.DeviceID(), model.PathRelay)
	waitPath(t, mac, remote.DeviceID(), model.PathRelay)
	waitPath(t, phone, mac.DeviceID(), model.PathLAN)
	settle(t, phone, remote)

	submitText(t, remote, "hello from the introduced device")
	waitClipboard(t, phone.clipboard, "hello from the introduced device")
}

// TestWakeRelayReachesEveryLink wakes every relay path that waits before its
// next attempt, not only one of them.
func TestWakeRelayReachesEveryLink(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	n := newRelayNode(t, network, bus, 23140, Config{})
	n.start(t)

	const waiters = 3
	done := make(chan bool, waiters)
	for range waiters {
		go func() { done <- n.pauseRelay(ctxOf(t), time.Hour) }()
	}
	// Let every waiter reach the select.
	time.Sleep(50 * time.Millisecond)
	n.wakeRelay()

	for i := range waiters {
		select {
		case ok := <-done:
			if !ok {
				t.Fatalf("waiter %d gave up instead of waking", i)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d relay waiters woke up", i, waiters)
		}
	}
}
