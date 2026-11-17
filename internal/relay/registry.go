package relay

import (
	"errors"
	"log/slog"
	"sync"
)

// maxMailboxMembers bounds the clients of one mailbox.
const maxMailboxMembers = 4

// errRegistryFull reports that the server holds as many mailboxes as allowed.
var errRegistryFull = errors.New("relay: mailbox registry full")

// mailboxID identifies a mailbox. The relay treats it as opaque.
type mailboxID [mailboxIDLen]byte

// registry stores the mailbox memberships in memory. A mailbox exists while at
// least one connection is a member of it.
type registry struct {
	metrics *metrics
	log     *slog.Logger

	mu     sync.Mutex
	boxes  map[mailboxID][]*peer
	byPeer map[*peer]map[mailboxID]struct{}
	limit  int
}

// newRegistry returns an empty registry.
func newRegistry(limit int, m *metrics, log *slog.Logger) *registry {
	return &registry{
		metrics: m,
		log:     log,
		boxes:   make(map[mailboxID][]*peer),
		byPeer:  make(map[*peer]map[mailboxID]struct{}),
		limit:   limit,
	}
}

// join adds a connection to the mailboxes. When a mailbox is full the oldest
// member is removed from it, so a client that reconnects wins over a stale
// connection.
func (r *registry) join(p *peer, ids []mailboxID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.reserve(ids); err != nil {
		return err
	}
	own := r.byPeer[p]
	if own == nil {
		own = make(map[mailboxID]struct{}, len(ids))
		r.byPeer[p] = own
	}
	for _, id := range ids {
		if _, ok := own[id]; ok {
			continue
		}
		members, existed := r.boxes[id]
		if len(members) >= maxMailboxMembers {
			evicted := members[0]
			r.log.Debug("mailbox member evicted",
				"conn", evicted.id,
				"transport", transportLabels[evicted.transport],
			)
			r.remove(evicted, id)
			members = r.boxes[id]
		}
		r.boxes[id] = append(members, p)
		own[id] = struct{}{}
		if !existed {
			r.metrics.mailboxes.Add(1)
		}
	}
	return nil
}

// reserve checks that the mailboxes fit into the limit.
func (r *registry) reserve(ids []mailboxID) error {
	created := 0
	seen := make(map[mailboxID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := r.boxes[id]; !ok {
			created++
		}
	}
	if len(r.boxes)+created > r.limit {
		return errRegistryFull
	}
	return nil
}

// remove drops one connection from one mailbox.
func (r *registry) remove(p *peer, id mailboxID) {
	members := r.boxes[id]
	for i, member := range members {
		if member != p {
			continue
		}
		members = append(members[:i], members[i+1:]...)
		if len(members) == 0 {
			delete(r.boxes, id)
			r.metrics.mailboxes.Add(-1)
		} else {
			r.boxes[id] = members
		}
		break
	}
	if own := r.byPeer[p]; own != nil {
		delete(own, id)
	}
}

// leave drops a connection from every mailbox.
func (r *registry) leave(p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	own := r.byPeer[p]
	delete(r.byPeer, p)
	for id := range own {
		r.remove(p, id)
	}
}

// member reports whether the connection joined the mailbox.
func (r *registry) member(p *peer, id mailboxID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byPeer[p][id]
	return ok
}

// targets returns the other members of a mailbox.
func (r *registry) targets(id mailboxID, from *peer) []*peer {
	r.mu.Lock()
	defer r.mu.Unlock()

	members := r.boxes[id]
	if len(members) == 0 {
		return nil
	}
	out := make([]*peer, 0, len(members))
	for _, member := range members {
		if member != from {
			out = append(out, member)
		}
	}
	return out
}
