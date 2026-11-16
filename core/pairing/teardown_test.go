package pairing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// Latency and number of exchanges of the teardown tests.
const (
	exchangeLatency = 20 * time.Millisecond
	exchangeCount   = 10
)

// pairAsUI drives a window like the user interface does: it confirms every
// request and closes the window as soon as an exchange ends. A window of a
// command closes right after the pair, which is when the connection of the
// exchange must already be free to close.
func pairAsUI(t *testing.T, window *Window) {
	t.Helper()
	go func() {
		for event := range window.Events() {
			switch event.Kind {
			case EventRequest:
				if err := window.Confirm(context.Background(), event.DeviceID); err != nil {
					t.Errorf("confirm %s: %v", event.DeviceID, err)
				}
			case EventPaired, EventFailed:
				_ = window.Close()
				return
			}
		}
	}()
}

// waitClosed waits until the window is closed.
func waitClosed(t *testing.T, window *Window) {
	t.Helper()
	select {
	case <-window.Done():
	case <-time.After(testTimeout):
		t.Fatal("the window stayed open")
	}
}

// TestScanPairsWithLatency checks repeated QR exchanges over a slow link. The
// showing window closes as soon as it reports the pair, so the accept frame has
// to be delivered before that event.
func TestScanPairsWithLatency(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	if err := network.SetLatency(exchangeLatency); err != nil {
		t.Fatalf("latency: %v", err)
	}

	for i := range exchangeCount {
		showing.open(t, ports.SystemClock, model.RolePersonal)
		code, err := showing.window.QR()
		if err != nil {
			t.Fatalf("exchange %d: code of the window: %v", i+1, err)
		}
		results := testScan(t, scanner, code)
		pairAsUI(t, showing.window)
		waitClosed(t, showing.window)
		if result := waitScan(t, results); result.err != nil {
			t.Fatalf("exchange %d: scan: %v", i+1, result.err)
		}
	}

	assertPeer(t, showing.trust, scanner, model.RolePersonal)
	assertPeer(t, scanner.trust, showing, model.RolePersonal)
}

// TestSASPairsWithLatency checks repeated SAS exchanges over a slow link with
// both windows closing as soon as they report the pair.
func TestSASPairsWithLatency(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	if err := network.SetLatency(exchangeLatency); err != nil {
		t.Fatalf("latency: %v", err)
	}

	for i := range exchangeCount {
		alice.open(t, ports.SystemClock, model.RolePersonal)
		bob.open(t, ports.SystemClock, model.RolePersonal)
		addr := bob.window.Addr().String()
		waitCandidate(t, alice.window, addr)

		pairing := dial(alice.window, addr)
		pairAsUI(t, alice.window)
		pairAsUI(t, bob.window)
		if err := <-pairing; err != nil {
			t.Fatalf("exchange %d: pairing: %v", i+1, err)
		}
		waitClosed(t, alice.window)
		waitClosed(t, bob.window)
	}

	assertPeer(t, alice.trust, bob, model.RolePersonal)
	assertPeer(t, bob.trust, alice, model.RolePersonal)
}

// TestSASRejectsWithLatency checks that a refusal reaches the other device
// whole even when the rejecting window closes right away.
func TestSASRejectsWithLatency(t *testing.T) {
	network, bus := testNetwork(t)
	alice := newNode(t, network, bus, 1, 41000)
	bob := newNode(t, network, bus, 2, 42000)
	alice.open(t, ports.SystemClock, model.RolePersonal)
	bob.open(t, ports.SystemClock, model.RolePersonal)
	if err := network.SetLatency(exchangeLatency); err != nil {
		t.Fatalf("latency: %v", err)
	}

	addr := bob.window.Addr().String()
	waitCandidate(t, alice.window, addr)

	pairing := dial(alice.window, addr)
	waitEvent(t, alice.window, EventRequest)
	waitEvent(t, bob.window, EventRequest)

	ctx := context.Background()
	if err := alice.window.Confirm(ctx, bob.deviceID()); err != nil {
		t.Fatalf("confirm on the initiator: %v", err)
	}
	if err := bob.window.Reject(ctx, alice.deviceID()); err != nil {
		t.Fatalf("reject on the responder: %v", err)
	}
	waitEvent(t, bob.window, EventFailed)
	if err := bob.window.Close(); err != nil {
		t.Fatalf("close the window: %v", err)
	}

	if err := <-pairing; !errors.Is(err, ErrRejected) {
		t.Fatalf("pairing error = %v, want %v", err, ErrRejected)
	}
	assertNoPeer(t, alice.trust, bob.deviceID())
	assertNoPeer(t, bob.trust, alice.deviceID())
}
