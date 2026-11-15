package nodetest

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
)

// sessionLog records the sessions of a device, so a wait can tell whether the
// connection with a peer has settled or is still being replaced.
type sessionLog struct {
	mu      sync.Mutex
	live    map[model.DeviceID]map[*sessionEntry]struct{}
	history []*sessionEntry
}

// sessionEntry is one session of a device.
type sessionEntry struct {
	peer    model.DeviceID
	started time.Time
	ended   time.Time
}

// newSessionLog creates an empty log.
func newSessionLog() *sessionLog {
	return &sessionLog{live: make(map[model.DeviceID]map[*sessionEntry]struct{})}
}

// add records a new session and follows it until it ends.
func (l *sessionLog) add(sess *session.Session) {
	entry := &sessionEntry{peer: sess.DeviceID(), started: time.Now()}

	l.mu.Lock()
	if l.live[entry.peer] == nil {
		l.live[entry.peer] = make(map[*sessionEntry]struct{})
	}
	l.live[entry.peer][entry] = struct{}{}
	l.history = append(l.history, entry)
	l.mu.Unlock()

	go func() {
		<-sess.Done()
		l.mu.Lock()
		delete(l.live[entry.peer], entry)
		entry.ended = time.Now()
		l.mu.Unlock()
	}()
}

// settled reports whether the peer has one session that lives for quiet. Two
// live sessions mean the devices are still replacing each other's connections.
func (l *sessionLog) settled(peer model.DeviceID, quiet time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.live[peer]) != 1 {
		return false
	}
	for entry := range l.live[peer] {
		return time.Since(entry.started) >= quiet
	}
	return false
}

// String describes the sessions in the order they started.
func (l *sessionLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	parts := make([]string, 0, len(l.history))
	for _, entry := range l.history {
		lifetime := now.Sub(entry.started)
		if !entry.ended.IsZero() {
			lifetime = entry.ended.Sub(entry.started)
		}
		parts = append(parts, fmt.Sprintf("%s %v", peerLabel(entry.peer), lifetime.Round(time.Millisecond)))
	}
	return strings.Join(parts, ", ")
}
