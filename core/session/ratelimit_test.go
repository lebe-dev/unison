package session

import (
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports/fake"
)

// TestTokenBucket checks the burst and the refill of the rate limiter.
func TestTokenBucket(t *testing.T) {
	clock := fake.NewClock(time.Unix(1000, 0))
	bucket := newTokenBucket(clock, 3, 2)

	for i := range 3 {
		if !bucket.allow() {
			t.Fatalf("token %d of the burst was refused", i+1)
		}
	}
	if bucket.allow() {
		t.Fatal("a token over the burst was allowed")
	}

	clock.Advance(500 * time.Millisecond)
	if !bucket.allow() {
		t.Fatal("a token after half a second was refused")
	}
	if bucket.allow() {
		t.Fatal("the bucket refilled more than the elapsed time allows")
	}

	clock.Advance(time.Hour)
	for i := range 3 {
		if !bucket.allow() {
			t.Fatalf("token %d after a long wait was refused", i+1)
		}
	}
	if bucket.allow() {
		t.Fatal("the bucket refilled over the burst")
	}
}
