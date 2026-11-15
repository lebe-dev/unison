package transfer

import (
	"sync"

	"github.com/lebe-dev/unison/core/model"
)

// source is one peer a fetch downloads from.
type source struct {
	peer Peer
	id   model.DeviceID
}

// sources is the set of peers a fetch may download from.
type sources struct {
	mu       sync.Mutex
	live     []source
	excluded []model.DeviceID
}

// newSources returns an empty set.
func newSources() *sources {
	return &sources{}
}

// add puts a peer into the set once.
func (s *sources) add(src source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, other := range s.live {
		if other.id == src.id {
			return
		}
	}
	s.live = append(s.live, src)
}

// available returns the peers that still serve the fetch.
func (s *sources) available() []source {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]source(nil), s.live...)
}

// drop removes a peer from the set.
func (s *sources) drop(id model.DeviceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, src := range s.live {
		if src.id != id {
			continue
		}
		s.live = append(s.live[:i], s.live[i+1:]...)
		s.excluded = append(s.excluded, id)
		return
	}
}

// failures returns the peers that were dropped.
func (s *sources) failures() []model.DeviceID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.DeviceID(nil), s.excluded...)
}

// hashLocks serializes the fetches of one blob.
type hashLocks struct {
	mu    sync.Mutex
	locks map[[32]byte]*hashLock
}

// hashLock is the lock of one blob.
type hashLock struct {
	mu   sync.Mutex
	refs int
}

// lock waits for the other fetches of the blob and returns the release
// function.
func (l *hashLocks) lock(hash [32]byte) func() {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[[32]byte]*hashLock)
	}
	held, ok := l.locks[hash]
	if !ok {
		held = &hashLock{}
		l.locks[hash] = held
	}
	held.refs++
	l.mu.Unlock()

	held.mu.Lock()
	return func() {
		held.mu.Unlock()

		l.mu.Lock()
		defer l.mu.Unlock()
		held.refs--
		if held.refs == 0 {
			delete(l.locks, hash)
		}
	}
}
