package node

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/discovery"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

const (
	// FriendsDiscoveryWindow is how long one friends discovery lasts.
	FriendsDiscoveryWindow = 15 * time.Minute
	// refusalLogInterval is how often refused connections are reported. The
	// count of the window goes into one line, so a stranger cannot flood the
	// log.
	refusalLogInterval = time.Minute
)

// ErrFriendsDiscoveryOff reports a connection of a Friends device while the
// device does not look for friends.
var ErrFriendsDiscoveryOff = errors.New("node: friends discovery is off")

// refusals counts the connections refused during the handshake.
type refusals struct {
	mu    sync.Mutex
	count int
	since time.Time
}

// add records one refusal and reports the count of the window when it ends.
func (r *refusals) add(now time.Time, window time.Duration) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.count++
	if !r.since.IsZero() && now.Sub(r.since) < window {
		return 0, false
	}
	count := r.count
	r.count = 0
	r.since = now
	return count, true
}

// FriendsDiscovery reports whether the device looks for Friends devices and
// until when.
func (n *Node) FriendsDiscovery(ctx context.Context) (bool, time.Time, error) {
	if err := n.usable(); err != nil {
		return false, time.Time{}, err
	}
	until, err := n.trust.FriendsDiscoveryUntil(ctx)
	if err != nil {
		return false, time.Time{}, err
	}
	if !n.clock.Now().Before(until) {
		return false, time.Time{}, nil
	}
	return true, until, nil
}

// SetFriendsDiscovery turns the search for Friends devices on or off. Turning it
// on while it runs extends the search by FriendsDiscoveryWindow from the later
// of now and the current end. It returns the end of the search, which is the
// zero time when it is off.
func (n *Node) SetFriendsDiscovery(ctx context.Context, on bool) (time.Time, error) {
	if err := n.usable(); err != nil {
		return time.Time{}, err
	}

	now := n.clock.Now()
	until := time.Time{}
	if on {
		stored, err := n.trust.FriendsDiscoveryUntil(ctx)
		if err != nil {
			return time.Time{}, err
		}
		base := now
		if stored.After(base) {
			base = stored
		}
		until = base.Add(FriendsDiscoveryWindow)
	}
	if err := n.trust.SetFriendsDiscoveryUntil(ctx, until); err != nil {
		return time.Time{}, err
	}

	n.wakeFriends()
	n.log.Info("friends discovery changed", "on", on, "until", until)
	return until, nil
}

// friendsOn reports whether the device looks for Friends devices.
func (n *Node) friendsOn() bool {
	n.friendsMu.Lock()
	defer n.friendsMu.Unlock()
	return n.clock.Now().Before(n.friendsUntil)
}

// setFriendsUntil stores the end of the search in memory.
func (n *Node) setFriendsUntil(until time.Time) {
	n.friendsMu.Lock()
	n.friendsUntil = until
	n.friendsMu.Unlock()
}

// wakeFriends tells the friends loop and the links to look at the mode again.
func (n *Node) wakeFriends() {
	select {
	case n.friendsSig <- struct{}{}:
	default:
	}
	for _, l := range n.linkList() {
		if l.role() == model.RoleFriends {
			l.retry()
		}
	}
}

// friendsLoop keeps the discovery in step with the stored end of the search.
func (n *Node) friendsLoop(ctx context.Context) {
	for {
		until, err := n.trust.FriendsDiscoveryUntil(ctx)
		if err != nil {
			n.log.Warn("reading the friends discovery mode failed", "error", err)
			until = time.Time{}
		}

		now := n.clock.Now()
		if !until.IsZero() && !now.Before(until) {
			n.log.Info("friends discovery ended")
			if err := n.trust.SetFriendsDiscoveryUntil(ctx, time.Time{}); err != nil {
				n.log.Warn("turning friends discovery off failed", "error", err)
			}
			until = time.Time{}
		}
		n.setFriendsUntil(until)

		if until.IsZero() {
			n.stopFriends()
			if !n.waitFriends(ctx, 0) {
				return
			}
			continue
		}

		n.startFriends(ctx)
		if !n.waitFriends(ctx, until.Sub(now)) {
			return
		}
	}
}

// waitFriends waits for the delay, a mode change or the end of the node. A zero
// delay waits for the change only.
func (n *Node) waitFriends(ctx context.Context, delay time.Duration) bool {
	var timer <-chan time.Time
	if delay > 0 {
		timer = n.clock.After(delay)
	}
	select {
	case <-ctx.Done():
		return false
	case <-n.friendsSig:
		return true
	case <-timer:
		return true
	}
}

// startFriends announces the device on the friends service and starts the
// search. It does nothing while the search already runs.
func (n *Node) startFriends(ctx context.Context) {
	n.friendsMu.Lock()
	running := n.friendsCancel != nil
	n.friendsMu.Unlock()
	if running {
		return
	}

	keys, err := n.trust.FriendsDiscoveryKeys(ctx)
	if err != nil {
		n.log.Warn("reading the friends keys failed", "error", err)
		return
	}

	browseCtx, cancel := context.WithCancel(ctx)
	n.friendsMu.Lock()
	n.friendsCancel = cancel
	n.friendsMu.Unlock()

	if len(keys) == 0 {
		n.log.Info("friends discovery is on without a friends device")
	} else {
		n.announceFriends(ctx)
	}
	n.spawn(func() { n.browseFriends(browseCtx) })
}

// stopFriends withdraws the announcement and stops the search. Live sessions
// stay.
func (n *Node) stopFriends() {
	n.friendsMu.Lock()
	cancel := n.friendsCancel
	n.friendsCancel = nil
	n.friendsMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()

	stopper, ok := n.cfg.Discovery.(serviceStopper)
	if !ok {
		n.log.Debug("the discovery cannot withdraw the friends announcement")
		return
	}
	if err := stopper.StopService(ports.ServiceFriends); err != nil {
		n.log.Warn("withdrawing the friends announcement failed", "error", err)
	}
	n.log.Info("friends discovery stopped")
}

// announceFriends announces the main listener on the friends service. The
// announcement carries only the addresses and the tags of the pairs.
func (n *Node) announceFriends(ctx context.Context) {
	if n.listener == nil {
		return
	}
	port := portOf(n.listener.Addr())
	if port == 0 {
		n.log.Warn("main listener has no port to announce")
		return
	}
	if err := n.cfg.Discovery.Advertise(ctx, ports.ServiceFriends, port, nil); err != nil {
		n.log.Warn("announcing the friends discovery failed", "error", err)
	}
}

// browseFriends dials the addresses discovery reports for the friends service.
// Only a candidate with a matching tag is dialed when discovery decides the
// match itself.
func (n *Node) browseFriends(ctx context.Context) {
	if tagged, ok := n.cfg.Discovery.(taggedDiscovery); ok {
		candidates, err := tagged.BrowseCandidates(ctx, ports.ServiceFriends)
		if err != nil {
			if ctx.Err() == nil {
				n.log.Warn("browsing for friends devices failed", "error", err)
			}
			return
		}
		consumeCandidates(ctx, candidates, func(candidate discovery.Candidate) {
			if !candidate.Matched {
				return
			}
			n.rememberCandidate(candidate.Addr)
			n.probeQuiet(ctx, candidate.Addr)
		})
		return
	}

	candidates, err := n.cfg.Discovery.Browse(ctx, ports.ServiceFriends)
	if err != nil {
		if ctx.Err() == nil {
			n.log.Warn("browsing for friends devices failed", "error", err)
		}
		return
	}
	consumeCandidates(ctx, candidates, func(candidate ports.Candidate) {
		n.rememberCandidate(candidate.Addr)
		n.probeQuiet(ctx, candidate.Addr)
	})
}

// countRefusal records a connection refused during the handshake.
func (n *Node) countRefusal() {
	count, report := n.refusals.add(n.clock.Now(), refusalLogInterval)
	if report {
		n.log.Info("connections from untrusted devices refused", "count", count)
	}
}
