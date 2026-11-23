package fake

import (
	"context"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

func TestDiscoveryTwoNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := NewBus()
	first := bus.Discovery()
	second := bus.Discovery()

	events, err := second.Browse(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}

	txt := map[string]string{"id": "abc"}
	if err := first.Advertise(ctx, ports.ServiceMain, 5000, txt); err != nil {
		t.Fatalf("advertise: %v", err)
	}

	got := receive(t, events)
	if got.Addr != "127.0.0.1:5000" {
		t.Fatalf("candidate addr: got %q, want %q", got.Addr, "127.0.0.1:5000")
	}
	if got.TXT["id"] != "abc" {
		t.Fatalf("candidate txt: got %v", got.TXT)
	}

	own, err := first.Browse(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse own: %v", err)
	}
	select {
	case candidate := <-own:
		t.Fatalf("node discovered its own advertisement: %+v", candidate)
	default:
	}
}

func TestDiscoveryBrowseAfterAdvertise(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := NewBus()
	advertiser := bus.Discovery()
	browser := bus.Discovery()

	if err := advertiser.Advertise(ctx, ports.ServicePairing, 6000, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}

	events, err := browser.Browse(ctx, ports.ServicePairing)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if got := receive(t, events); got.Addr != "127.0.0.1:6000" {
		t.Fatalf("candidate addr: got %q, want %q", got.Addr, "127.0.0.1:6000")
	}
}

func TestDiscoveryServiceIsolation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := NewBus()
	advertiser := bus.Discovery()
	browser := bus.Discovery()

	events, err := browser.Browse(ctx, ports.ServicePairing)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if err := advertiser.Advertise(ctx, ports.ServiceMain, 5000, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}

	select {
	case candidate := <-events:
		t.Fatalf("candidate from another service: %+v", candidate)
	default:
	}
}

func TestDiscoveryStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := NewBus()
	node := bus.Discovery()

	events, err := node.Browse(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if err := node.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitCandidateClosed(t, events)
}

func receive(t *testing.T, events <-chan ports.Candidate) ports.Candidate {
	t.Helper()
	select {
	case candidate, ok := <-events:
		if !ok {
			t.Fatal("browse channel closed")
		}
		return candidate
	case <-time.After(time.Second):
		t.Fatal("no candidate received")
		return ports.Candidate{}
	}
}

func waitCandidateClosed(t *testing.T, events <-chan ports.Candidate) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("browse channel was not closed")
		}
	}
}
