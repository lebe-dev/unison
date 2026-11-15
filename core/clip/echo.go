package clip

import (
	"sync"
	"time"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/ports"
)

// echoGuard holds the hashes of content written to the clipboard. The watcher
// reports those changes back, so they must not become new items.
type echoGuard struct {
	mu     sync.Mutex
	clock  ports.Clock
	window time.Duration
	seen   map[[32]byte]time.Time
}

// newEchoGuard creates a guard that forgets hashes after window.
func newEchoGuard(clock ports.Clock, window time.Duration) *echoGuard {
	return &echoGuard{
		clock:  clock,
		window: window,
		seen:   make(map[[32]byte]time.Time),
	}
}

// register remembers the payload hashes of content until the echo window ends.
func (g *echoGuard) register(content ports.Content) {
	expiry := g.clock.Now().Add(g.window)

	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock.Now()
	for hash, at := range g.seen {
		if !now.Before(at) {
			delete(g.seen, hash)
		}
	}
	for _, rep := range content.Representations {
		if len(rep.Data) == 0 {
			continue
		}
		g.seen[blake3.Sum256(rep.Data)] = expiry
	}
}

// match reports whether any payload of content was written by this pipeline
// inside the echo window.
func (g *echoGuard) match(content ports.Content) bool {
	now := g.clock.Now()

	g.mu.Lock()
	defer g.mu.Unlock()
	for _, rep := range content.Representations {
		if len(rep.Data) == 0 {
			continue
		}
		hash := blake3.Sum256(rep.Data)
		expiry, ok := g.seen[hash]
		if !ok {
			continue
		}
		if now.Before(expiry) {
			return true
		}
		delete(g.seen, hash)
	}
	return false
}
