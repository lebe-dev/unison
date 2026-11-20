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

// errPeerGone reports a connection that already left its mailboxes.
var errPeerGone = errors.New("relay: connection left its mailboxes")

// mailboxID identifies a mailbox. The relay treats it as opaque.
type mailboxID [mailboxIDLen]byte

// mailboxKey identifies a mailbox inside the namespace of one token. The same
// mailbox id under two tokens is two unrelated mailboxes.
type mailboxKey struct {
	tenant tenantID
	id     mailboxID
}

// registry stores the mailbox memberships in memory. A mailbox exists while at
// least one connection is a member of it. Mailboxes of different tokens never
// share a member list, so one token cannot observe or join the traffic of
// another.
type registry struct {
	metrics *metrics
	log     *slog.Logger

	mu     sync.Mutex
	boxes  map[mailboxKey][]*peer
	byPeer map[*peer]map[mailboxKey]struct{}
	limit  int
}

// newRegistry returns an empty registry.
func newRegistry(limit int, m *metrics, log *slog.Logger) *registry {
	return &registry{
		metrics: m,
		log:     log,
		boxes:   make(map[mailboxKey][]*peer),
		byPeer:  make(map[*peer]map[mailboxKey]struct{}),
		limit:   limit,
	}
}

// mailboxKeys scopes mailbox ids to the namespace of a token.
func mailboxKeys(tenant tenantID, ids []mailboxID) []mailboxKey {
	keys := make([]mailboxKey, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, mailboxKey{tenant: tenant, id: id})
	}
	return keys
}

// join adds a connection to the mailboxes of its token. When a mailbox is full
// the oldest member is removed from it, so a client that reconnects wins over a
// stale connection. The eviction stays inside the token.
func (r *registry) join(p *peer, tenant tenantID, ids []mailboxID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	keys := mailboxKeys(tenant, ids)
	if err := r.reserve(keys); err != nil {
		return err
	}
	r.joinLocked(p, keys)
	return nil
}

// replace makes the mailboxes the whole set of a registered connection. On a
// failure the connection keeps its previous set.
func (r *registry) replace(p *peer, tenant tenantID, ids []mailboxID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	own, ok := r.byPeer[p]
	if !ok {
		return errPeerGone
	}
	keys := mailboxKeys(tenant, ids)
	if err := r.reserve(keys); err != nil {
		return err
	}
	want := make(map[mailboxKey]struct{}, len(keys))
	for _, key := range keys {
		want[key] = struct{}{}
	}
	for key := range own {
		if _, keep := want[key]; !keep {
			r.remove(p, key)
		}
	}
	r.joinLocked(p, keys)
	return nil
}

// joinLocked adds a connection to the mailboxes. The caller must hold r.mu and
// have reserved the mailboxes.
func (r *registry) joinLocked(p *peer, keys []mailboxKey) {
	own := r.byPeer[p]
	if own == nil {
		own = make(map[mailboxKey]struct{}, len(keys))
		r.byPeer[p] = own
	}
	for _, key := range keys {
		if _, ok := own[key]; ok {
			continue
		}
		members, existed := r.boxes[key]
		if len(members) >= maxMailboxMembers {
			evicted := members[0]
			r.log.Debug("mailbox member evicted",
				"conn", evicted.id,
				"transport", transportLabels[evicted.transport],
			)
			r.remove(evicted, key)
			members = r.boxes[key]
		}
		r.boxes[key] = append(members, p)
		own[key] = struct{}{}
		if !existed {
			r.metrics.mailboxes.Add(1)
		}
	}
}

// reserve checks that the mailboxes fit into the limit.
func (r *registry) reserve(keys []mailboxKey) error {
	created := 0
	seen := make(map[mailboxKey]struct{}, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := r.boxes[key]; !ok {
			created++
		}
	}
	if len(r.boxes)+created > r.limit {
		return errRegistryFull
	}
	return nil
}

// remove drops one connection from one mailbox.
func (r *registry) remove(p *peer, key mailboxKey) {
	members := r.boxes[key]
	for i, member := range members {
		if member != p {
			continue
		}
		members = append(members[:i], members[i+1:]...)
		if len(members) == 0 {
			delete(r.boxes, key)
			r.metrics.mailboxes.Add(-1)
		} else {
			r.boxes[key] = members
		}
		break
	}
	if own := r.byPeer[p]; own != nil {
		delete(own, key)
	}
}

// leave drops a connection from every mailbox.
func (r *registry) leave(p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	own := r.byPeer[p]
	delete(r.byPeer, p)
	for key := range own {
		r.remove(p, key)
	}
}

// member reports whether the connection joined the mailbox of its token.
func (r *registry) member(p *peer, tenant tenantID, id mailboxID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byPeer[p][mailboxKey{tenant: tenant, id: id}]
	return ok
}

// targets returns the other members of a mailbox of the token.
func (r *registry) targets(tenant tenantID, id mailboxID, from *peer) []*peer {
	r.mu.Lock()
	defer r.mu.Unlock()

	members := r.boxes[mailboxKey{tenant: tenant, id: id}]
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
