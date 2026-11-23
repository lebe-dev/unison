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
// Binary payloads and file paths are keyed the same way they are observed.
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
		if key, ok := payloadKey(rep); ok {
			g.seen[key] = expiry
		}
	}
}

// match reports whether any payload of content was written by this pipeline
// inside the echo window.
func (g *echoGuard) match(content ports.Content) bool {
	now := g.clock.Now()

	g.mu.Lock()
	defer g.mu.Unlock()
	for _, rep := range content.Representations {
		key, ok := payloadKey(rep)
		if !ok {
			continue
		}
		expiry, ok := g.seen[key]
		if !ok {
			continue
		}
		if now.Before(expiry) {
			return true
		}
		delete(g.seen, key)
	}
	return false
}

// payloadKey returns the hash that identifies one payload.
func payloadKey(rep ports.Representation) ([32]byte, bool) {
	if len(rep.Data) > 0 {
		return blake3.Sum256(rep.Data), true
	}
	if rep.FilePath != "" {
		return blake3.Sum256([]byte(rep.FilePath)), true
	}
	return [32]byte{}, false
}
