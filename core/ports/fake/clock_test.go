package fake

import (
	"testing"
	"time"
)

func TestClock(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := NewClock(start)

	if got := clock.Now(); !got.Equal(start) {
		t.Fatalf("now: got %v, want %v", got, start)
	}

	fired := clock.After(5 * time.Second)
	clock.Advance(4 * time.Second)
	select {
	case got := <-fired:
		t.Fatalf("timer fired too early at %v", got)
	default:
	}

	clock.Advance(time.Second)
	select {
	case got := <-fired:
		if want := start.Add(5 * time.Second); !got.Equal(want) {
			t.Fatalf("timer fired at %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timer did not fire")
	}
}

func TestClockAfterNonPositive(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := NewClock(start)

	select {
	case got := <-clock.After(0):
		if !got.Equal(start) {
			t.Fatalf("timer fired at %v, want %v", got, start)
		}
	default:
		t.Fatal("timer with zero delay did not fire")
	}
}

func TestClockWaiters(t *testing.T) {
	clock := NewClock(time.Unix(0, 0))
	early := clock.After(time.Second)
	late := clock.After(3 * time.Second)

	clock.Advance(2 * time.Second)
	select {
	case <-early:
	default:
		t.Fatal("early timer did not fire")
	}
	select {
	case <-late:
		t.Fatal("late timer fired too early")
	default:
	}

	clock.Advance(time.Second)
	select {
	case <-late:
	default:
		t.Fatal("late timer did not fire")
	}
}
