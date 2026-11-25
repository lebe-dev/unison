package node

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/relay"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	relayserver "github.com/lebe-dev/unison/internal/relay"
)

// startMultiTokenRelay runs an in-process relay server accepting all tokens.
func startMultiTokenRelay(t *testing.T, tokens ...string) *nodeTestRelay {
	t.Helper()

	certFile, keyFile := nodeTestCertificate(t)
	address := fmt.Sprintf("127.0.0.1:%d", nodeTestPort(t))

	cfg := relayserver.DefaultConfig()
	cfg.QUICAddr = address
	cfg.WSAddr = address
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	cfg.Tokens = tokens
	cfg.MaxDatagramBytes = 1400
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	server, err := relayserver.New(cfg)
	if err != nil {
		t.Fatalf("new relay: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	r := &nodeTestRelay{url: address, certFile: certFile, keyFile: keyFile, server: server}
	t.Cleanup(func() {
		if r.server != nil {
			if err := r.server.Close(); err != nil {
				t.Errorf("close relay: %v", err)
			}
		}
	})
	return r
}

func setNodeRelay(t *testing.T, n *testNode, url, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.SetRelay(ctx, trust.RelaySettings{URL: url, Token: token}); err != nil {
		t.Fatalf("set relay: %v", err)
	}
}

func waitRelayCandidate(t *testing.T, n *testNode) PairingCandidate {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		for _, c := range n.PairingCandidates() {
			if pairing.IsRelayAddr(c.Addr) {
				return c
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no relay pairing candidate on %s within %v", n.DeviceID(), testTimeout)
	return PairingCandidate{}
}

func waitNodeEvent(t *testing.T, events <-chan Event, kind EventKind) Event {
	t.Helper()
	timer := time.NewTimer(testTimeout)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed while waiting for %s", kind)
			}
			if ev.Kind == kind {
				return ev
			}
		case <-timer.C:
			t.Fatalf("no %s event within %v", kind, testTimeout)
		}
	}
}

func TestRelayPairingDiscoveryAndSAS(t *testing.T) {
	server := startMultiTokenRelay(t, "token-alpha")
	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}

	// Separate networks and discovery buses: local discovery cannot see each other.
	a := newRelayNode(t, vnet.NewNetwork(), fake.NewBus(), 22000, Config{Relay: trusted})
	b := newRelayNode(t, vnet.NewNetwork(), fake.NewBus(), 22004, Config{Relay: trusted})
	a.start(t)
	b.start(t)
	a.sockets.disconnect()
	b.sockets.disconnect()

	setNodeRelay(t, a, server.url, "token-alpha")
	setNodeRelay(t, b, server.url, "token-alpha")

	subA := a.Subscribe()
	defer subA.Close()
	subB := b.Subscribe()
	defer subB.Close()

	if err := a.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open pairing A: %v", err)
	}
	defer func() { _ = a.ClosePairing() }()
	if err := b.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open pairing B: %v", err)
	}
	defer func() { _ = b.ClosePairing() }()

	candA := waitRelayCandidate(t, a)
	candB := waitRelayCandidate(t, b)
	if candA.TXT["transport"] != "relay" {
		t.Fatalf("candidate on A missing relay transport TXT: %+v", candA)
	}
	if candB.TXT["transport"] != "relay" {
		t.Fatalf("candidate on B missing relay transport TXT: %+v", candB)
	}

	if err := a.StartPairing(candA.Addr); err != nil {
		t.Fatalf("start pairing A: %v", err)
	}

	reqA := waitNodeEvent(t, subA.Events(), EventPairingRequest)
	reqB := waitNodeEvent(t, subB.Events(), EventPairingRequest)
	if reqA.SAS.String() != reqB.SAS.String() {
		t.Fatalf("SAS mismatch: A=%q B=%q", reqA.SAS.String(), reqB.SAS.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := a.ConfirmPairing(ctx, reqA.Peer); err != nil {
		t.Fatalf("confirm pairing A: %v", err)
	}
	if err := b.ConfirmPairing(ctx, reqB.Peer); err != nil {
		t.Fatalf("confirm pairing B: %v", err)
	}

	waitNodeEvent(t, subA.Events(), EventPairingPaired)
	waitNodeEvent(t, subB.Events(), EventPairingPaired)

	if _, err := a.Trust().Get(ctx, b.DeviceID()); err != nil {
		t.Fatalf("A did not store B: %v", err)
	}
	if _, err := b.Trust().Get(ctx, a.DeviceID()); err != nil {
		t.Fatalf("B did not store A: %v", err)
	}
}

func TestRelayPairingTokenIsolation(t *testing.T) {
	server := startMultiTokenRelay(t, "token-alpha", "token-beta")
	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}
	shortHandshake := transport.Config{HandshakeTimeout: 600 * time.Millisecond}

	a := newRelayNode(t, vnet.NewNetwork(), fake.NewBus(), 22010, Config{Relay: trusted, Transport: shortHandshake})
	b := newRelayNode(t, vnet.NewNetwork(), fake.NewBus(), 22014, Config{Relay: trusted, Transport: shortHandshake})
	c := newRelayNode(t, vnet.NewNetwork(), fake.NewBus(), 22018, Config{Relay: trusted, Transport: shortHandshake})
	a.start(t)
	b.start(t)
	c.start(t)
	a.sockets.disconnect()
	b.sockets.disconnect()
	c.sockets.disconnect()

	setNodeRelay(t, a, server.url, "token-alpha")
	setNodeRelay(t, b, server.url, "token-alpha")
	setNodeRelay(t, c, server.url, "token-beta")

	subA := a.Subscribe()
	defer subA.Close()
	subC := c.Subscribe()
	defer subC.Close()

	if err := a.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open pairing A: %v", err)
	}
	defer func() { _ = a.ClosePairing() }()
	if err := b.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open pairing B: %v", err)
	}
	defer func() { _ = b.ClosePairing() }()
	if err := c.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open pairing C: %v", err)
	}
	defer func() { _ = c.ClosePairing() }()

	// Wait until A and B discover each other, then let beacons settle.
	candOnA := waitRelayCandidate(t, a)
	candOnB := waitRelayCandidate(t, b)
	time.Sleep(1500 * time.Millisecond)

	if got := a.PairingCandidates(); len(got) != 1 || got[0].Addr != candOnA.Addr {
		t.Fatalf("A candidates = %+v, want only B (%s)", got, candOnA.Addr)
	}
	if got := b.PairingCandidates(); len(got) != 1 || got[0].Addr != candOnB.Addr {
		t.Fatalf("B candidates = %+v, want only A (%s)", got, candOnB.Addr)
	}
	if got := c.PairingCandidates(); len(got) != 0 {
		t.Fatalf("C on different token saw candidates: %+v", got)
	}

	// Even if C guesses A's relay address, dialing must fail and never reach A.
	if err := c.StartPairing(candOnB.Addr); err != nil {
		t.Fatalf("start pairing C: %v", err)
	}
	waitNodeEvent(t, subC.Events(), EventPairingFailed)

	for {
		select {
		case ev := <-subA.Events():
			if ev.Kind == EventPairingRequest || ev.Kind == EventPairingPaired {
				t.Fatalf("A unexpectedly received pairing event from C: %+v", ev)
			}
		default:
			return
		}
	}
}

func TestRelayPairingDisabledWhenNotConfigured(t *testing.T) {
	a := newRelayNode(t, vnet.NewNetwork(), fake.NewBus(), 22024, Config{})
	a.start(t)

	if err := a.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open pairing without relay: %v", err)
	}
	defer func() { _ = a.ClosePairing() }()

	time.Sleep(150 * time.Millisecond)
	if got := a.PairingCandidates(); len(got) != 0 {
		t.Fatalf("unexpected candidates without relay: %+v", got)
	}
}
