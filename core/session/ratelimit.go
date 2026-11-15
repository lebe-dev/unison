package session

import (
	"math"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// tokenBucket limits a message rate with a burst allowance.
type tokenBucket struct {
	clock  ports.Clock
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

// newTokenBucket returns a bucket that starts full.
func newTokenBucket(clock ports.Clock, burst, ratePerSecond int) *tokenBucket {
	return &tokenBucket{
		clock:  clock,
		rate:   float64(ratePerSecond),
		burst:  float64(burst),
		tokens: float64(burst),
	}
}

// allow takes one token and reports whether it was available.
func (b *tokenBucket) allow() bool {
	now := b.clock.Now()
	if !b.last.IsZero() {
		if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
			b.tokens = math.Min(b.burst, b.tokens+elapsed*b.rate)
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
