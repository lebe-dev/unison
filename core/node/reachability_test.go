package node

import (
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// fastTransport makes a lost connection end within a second.
func fastTransport(cfg *Config) {
	cfg.Transport = transport.Config{
		KeepAlive:        100 * time.Millisecond,
		IdleTimeout:      time.Second,
		HandshakeTimeout: time.Second,
	}
	cfg.Limits.DialTimeout = time.Second
}

func TestPeerUnreachableWhenTheNetworkDropsIt(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	a := newTestNodeWith(t, network, bus, 23000, t.TempDir(), fake.NewKeystore(), fastTransport)
	b := newTestNodeWith(t, network, bus, 23004, t.TempDir(), fake.NewKeystore(), fastTransport)
	a.start(t)
	b.start(t)
	events := a.subscribe(t)
	a.pair(t, b)
	b.pair(t, a)
	waitEvent(t, events, EventPeerOnline, b.DeviceID())

	// Every datagram of b is lost: a still knows its address, but b never
	// answers, as on a network that keeps its clients apart.
	b.sockets.disconnect()
	unreachable := waitEvent(t, events, EventPeerUnreachable, b.DeviceID())
	if unreachable.Err == nil || !mentionsAny(unreachable.Err.Error(), b.sockets.boundAddrs()) {
		t.Errorf("unreachable error = %v, want an address of %v", unreachable.Err, b.sockets.boundAddrs())
	}

	state, err := a.Peer(ctxOf(t), b.DeviceID())
	if err != nil {
		t.Fatalf("peer state: %v", err)
	}
	if state.Online || !state.Unreachable || state.UnreachableSince.IsZero() {
		t.Fatalf("state = %+v, want an unreachable offline peer", state)
	}
	if !mentionsAny(state.UnreachableDetail, b.sockets.boundAddrs()) {
		t.Errorf("detail = %q, want an address of %v", state.UnreachableDetail, b.sockets.boundAddrs())
	}

	b.sockets.reconnect()
	waitEvent(t, events, EventPeerOnline, b.DeviceID())
	state, err = a.Peer(ctxOf(t), b.DeviceID())
	if err != nil {
		t.Fatalf("peer state: %v", err)
	}
	if !state.Online || state.Unreachable || state.UnreachableDetail != "" {
		t.Errorf("state after the network came back = %+v, want an online peer", state)
	}
}

// mentionsAny reports whether text names one of the addresses.
func mentionsAny(text string, addrs []string) bool {
	for _, addr := range addrs {
		if strings.Contains(text, addr) {
			return true
		}
	}
	return false
}
