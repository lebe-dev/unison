package nodetest

import (
	"time"

	"github.com/lebe-dev/unison/core/ports/fake"
)

// virtualClock is a fake clock that advances on its own. Timers inside the
// nodes therefore fire while a test waits on real time, and a test can still
// skip over a long delay with Advance.
type virtualClock struct {
	clock *fake.Clock
	step  time.Duration
	stop  chan struct{}
	done  chan struct{}
}

// newVirtualClock creates a clock set to start that advances by step per tick.
func newVirtualClock(start time.Time, step time.Duration) *virtualClock {
	c := &virtualClock{
		clock: fake.NewClock(start),
		step:  step,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go c.run()
	return c
}

// run advances the clock until close.
func (c *virtualClock) run() {
	defer close(c.done)

	ticker := time.NewTicker(c.step)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.clock.Advance(c.step)
		}
	}
}

// Now returns the current time of the clock.
func (c *virtualClock) Now() time.Time {
	return c.clock.Now()
}

// After returns a channel that receives the time once d has elapsed on the
// clock.
func (c *virtualClock) After(d time.Duration) <-chan time.Time {
	return c.clock.After(d)
}

// Advance moves the clock forward by d and fires the waiting timers.
func (c *virtualClock) Advance(d time.Duration) {
	c.clock.Advance(d)
}

// close stops the ticking goroutine.
func (c *virtualClock) close() {
	close(c.stop)
	<-c.done
}
