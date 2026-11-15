package relay

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math"
	"sync"
	"time"
)

// Errors of the token accounting.
var (
	errRateLimited   = errors.New("relay: rate limited")
	errQuotaExceeded = errors.New("relay: quota exceeded")
)

// tokenBucket is the datagram rate of one token.
type tokenBucket struct {
	perSecond float64
	burst     float64
	tokens    float64
	last      time.Time
}

// newTokenBucket returns a bucket that starts full.
func newTokenBucket(perSecond, burst int, now time.Time) *tokenBucket {
	burst = max(burst, 1)
	return &tokenBucket{
		perSecond: float64(perSecond),
		burst:     float64(burst),
		tokens:    float64(burst),
		last:      now,
	}
}

// take removes one token when the bucket holds one.
func (b *tokenBucket) take(now time.Time) bool {
	if now.After(b.last) {
		elapsed := now.Sub(b.last).Seconds()
		b.tokens = math.Min(b.burst, b.tokens+elapsed*b.perSecond)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// tokenState is the traffic accounting of one access token. The state is shared
// by every connection that presents the token.
type tokenState struct {
	hash   [sha256.Size]byte
	quota  uint64
	window time.Duration

	mu      sync.Mutex
	bucket  *tokenBucket
	used    uint64
	started time.Time
}

// newTokenState hashes a token and starts its quota window.
func newTokenState(token string, cfg Config, now time.Time) *tokenState {
	return &tokenState{
		hash:    sha256.Sum256([]byte(token)),
		quota:   uint64(cfg.QuotaBytes),
		window:  cfg.QuotaWindow,
		bucket:  newTokenBucket(cfg.RatePerSecond, cfg.RateBurst, now),
		started: now,
	}
}

// take accounts one datagram of size bytes. A datagram that the quota or the
// rate rejects is not counted as traffic.
func (t *tokenState) take(now time.Time, size int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.window > 0 && now.Sub(t.started) >= t.window {
		t.used = 0
		t.started = now
	}
	if t.used >= t.quota || uint64(size) > t.quota-t.used {
		return errQuotaExceeded
	}
	if !t.bucket.take(now) {
		return errRateLimited
	}
	t.used += uint64(size)
	return nil
}

// tokenSet holds the access tokens of the server.
type tokenSet struct {
	states []*tokenState
}

// newTokenSet hashes the tokens. Every token keeps its own accounting.
func newTokenSet(tokens []string, cfg Config, now time.Time) *tokenSet {
	states := make([]*tokenState, 0, len(tokens))
	for _, token := range tokens {
		states = append(states, newTokenState(token, cfg, now))
	}
	return &tokenSet{states: states}
}

// find returns the accounting of a token, or nil when the token is unknown. The
// hash of every token is compared, so the lookup does not reveal a prefix.
func (s *tokenSet) find(token []byte) *tokenState {
	sum := sha256.Sum256(token)
	var found *tokenState
	for _, state := range s.states {
		if subtle.ConstantTimeCompare(sum[:], state.hash[:]) == 1 {
			found = state
		}
	}
	return found
}
