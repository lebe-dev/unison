package node

import (
	"log/slog"
	"testing"
	"time"
)

// TestEventBusDeliversToEverySubscriber checks that each subscriber gets its own
// copy of the events.
func TestEventBusDeliversToEverySubscriber(t *testing.T) {
	bus := newEventBus(4, slog.New(slog.DiscardHandler))
	first := bus.subscribe()
	second := bus.subscribe()
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)

	bus.publish(Event{Kind: EventPeerOnline})
	bus.publish(Event{Kind: EventClipboard})

	for _, subscriber := range []*Subscriber{first, second} {
		for _, want := range []EventKind{EventPeerOnline, EventClipboard} {
			select {
			case event := <-subscriber.Events():
				if event.Kind != want {
					t.Fatalf("event is %s, want %s", event.Kind, want)
				}
				if event.At.IsZero() {
					t.Fatal("the event has no time")
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("no %s event", want)
			}
		}
	}
}

// TestEventBusSlowSubscriber checks that a full queue drops events instead of
// blocking the publisher.
func TestEventBusSlowSubscriber(t *testing.T) {
	bus := newEventBus(1, slog.New(slog.DiscardHandler))
	slow := bus.subscribe()
	t.Cleanup(slow.Close)
	fast := bus.subscribe()
	t.Cleanup(fast.Close)

	published := make(chan struct{})
	go func() {
		for range 10 {
			bus.publish(Event{Kind: EventClipboard})
		}
		close(published)
	}()

	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("the publisher blocked on a slow subscriber")
	}

	select {
	case event := <-fast.Events():
		if event.Kind != EventClipboard {
			t.Fatalf("event is %s, want %s", event.Kind, EventClipboard)
		}
	default:
		t.Fatal("the queue of the reading subscriber is empty")
	}

	// Closing the slow subscriber releases its queue, and publishing again must
	// not write to the closed channel.
	slow.Close()
	bus.publish(Event{Kind: EventPeerOnline})
}
