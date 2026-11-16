package node

import (
	"context"
	"errors"
	"maps"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/discovery"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// friendsNet is a discovery bus that tags the announcements of the friends
// service like the multicast discovery does.
type friendsNet struct {
	mu    sync.Mutex
	nodes []*friendsDiscovery
}

// join adds the discovery instance of one node.
func (f *friendsNet) join() *friendsDiscovery {
	f.mu.Lock()
	defer f.mu.Unlock()

	d := &friendsDiscovery{
		net:      f,
		ads:      make(map[ports.Service]friendsAd),
		watchers: make(map[int]*friendsWatch),
	}
	f.nodes = append(f.nodes, d)
	return d
}

// friendsAnnouncements returns the number of nodes that announce the friends
// service.
func (f *friendsNet) friendsAnnouncements() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	count := 0
	for _, node := range f.nodes {
		if _, ok := node.ads[ports.ServiceFriends]; ok {
			count++
		}
	}
	return count
}

// friendsAd is one announcement of the bus.
type friendsAd struct {
	addr  string
	txt   map[string]string
	owner *friendsDiscovery
}

// friendsWatch is one browser of the bus.
type friendsWatch struct {
	service ports.Service
	ch      chan discovery.Candidate
}

// friendsDiscovery is one ports.Discovery of the bus.
type friendsDiscovery struct {
	net *friendsNet
	// node provides the keys the tags are derived from. It is set before the
	// node starts.
	node *testNode

	// ads and watchers are guarded by net.mu.
	ads      map[ports.Service]friendsAd
	watchers map[int]*friendsWatch
	next     int
	stopped  bool
}

var (
	_ ports.Discovery = (*friendsDiscovery)(nil)
	_ taggedDiscovery = (*friendsDiscovery)(nil)
	_ serviceStopper  = (*friendsDiscovery)(nil)
)

// Advertise announces a service instance.
func (d *friendsDiscovery) Advertise(ctx context.Context, service ports.Service, port int, txt map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	ad := friendsAd{
		addr:  net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		txt:   maps.Clone(txt),
		owner: d,
	}

	bus := d.net
	bus.mu.Lock()
	defer bus.mu.Unlock()
	d.ads[service] = ad
	for _, node := range bus.nodes {
		if node == d {
			continue
		}
		for _, w := range node.watchers {
			if w.service == service {
				deliverFriend(w, ad, node, d.now())
			}
		}
	}
	return nil
}

// Browse delivers the instances of the service as ports.Candidate.
func (d *friendsDiscovery) Browse(ctx context.Context, service ports.Service) (<-chan ports.Candidate, error) {
	tagged, err := d.BrowseCandidates(ctx, service)
	if err != nil {
		return nil, err
	}

	out := make(chan ports.Candidate, candidateBufferTest)
	go func() {
		defer close(out)
		for c := range tagged {
			select {
			case out <- ports.Candidate{Addr: c.Addr, TXT: c.TXT}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// candidateBufferTest is the buffer of the browse channels of the test bus.
const candidateBufferTest = 16

// BrowseCandidates delivers the instances of the service with the tag verdict.
func (d *friendsDiscovery) BrowseCandidates(ctx context.Context, service ports.Service) (<-chan discovery.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bus := d.net
	bus.mu.Lock()
	id := d.next
	d.next++
	w := &friendsWatch{service: service, ch: make(chan discovery.Candidate, candidateBufferTest)}
	d.watchers[id] = w
	for _, node := range bus.nodes {
		if node == d {
			continue
		}
		if ad, ok := node.ads[service]; ok {
			deliverFriend(w, ad, d, d.now())
		}
	}
	bus.mu.Unlock()

	go func() {
		<-ctx.Done()
		bus.mu.Lock()
		defer bus.mu.Unlock()
		if _, ok := d.watchers[id]; !ok {
			return
		}
		delete(d.watchers, id)
		close(w.ch)
	}()
	return w.ch, nil
}

// StopService withdraws one announcement.
func (d *friendsDiscovery) StopService(service ports.Service) error {
	bus := d.net
	bus.mu.Lock()
	defer bus.mu.Unlock()
	delete(d.ads, service)
	return nil
}

// Stop removes every announcement and closes every browser.
func (d *friendsDiscovery) Stop() error {
	bus := d.net
	bus.mu.Lock()
	defer bus.mu.Unlock()
	d.stopped = true
	clear(d.ads)
	for id := range d.watchers {
		close(d.watchers[id].ch)
		delete(d.watchers, id)
	}
	return nil
}

// now returns the time the tags are computed for.
func (d *friendsDiscovery) now() time.Time {
	return time.Now()
}

// keysOf returns the keys that tag the announcements of the service.
func (d *friendsDiscovery) keysOf(service ports.Service) [][]byte {
	if d.node == nil {
		return nil
	}

	ctx := context.Background()
	var (
		keys [][]byte
		err  error
	)
	if service == ports.ServiceFriends {
		keys, err = d.node.Trust().FriendsDiscoveryKeys(ctx)
	} else {
		keys, err = d.node.Trust().DiscoveryKeys(ctx)
	}
	if err != nil {
		return nil
	}
	return keys
}

// deliverFriend passes one announcement to a browser. The pairing service has no
// tag, the other services are matched by the tags of their keys.
func deliverFriend(w *friendsWatch, ad friendsAd, browser *friendsDiscovery, now time.Time) {
	matched := true
	if w.service == ports.ServiceMain || w.service == ports.ServiceFriends {
		matched = matchKeys(browser.keysOf(w.service), ad.owner.keysOf(w.service), now)
	}
	select {
	case w.ch <- discovery.Candidate{Addr: ad.addr, TXT: maps.Clone(ad.txt), Matched: matched}:
	default:
	}
}

// matchKeys reports whether a tag of the announcement keys matches a key of the
// browser.
func matchKeys(browser, announced [][]byte, now time.Time) bool {
	for _, key := range announced {
		tag := discovery.Tag(key, now)
		if tag == "" {
			continue
		}
		if discovery.MatchAnyTag(browser, tag, now) {
			return true
		}
	}
	return false
}

// friendsPair is a pair of Friends nodes that share a network and a clock.
type friendsPair struct {
	network *vnet.Network
	bus     *friendsNet
	a, b    *testNode
}

// newFriendsPair starts two Friends devices on the test bus. The devices are
// paired but have no addresses of each other, so they have to find each other
// with discovery.
func newFriendsPair(t *testing.T, base int) *friendsPair {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := &friendsNet{}

	a := newFriendsNode(t, network, bus, base, t.TempDir(), fake.NewKeystore(), nil)
	b := newFriendsNode(t, network, bus, base+1000, t.TempDir(), fake.NewKeystore(), nil)
	a.start(t)
	b.start(t)

	pairFriends(t, a, b)
	pairFriends(t, b, a)
	return &friendsPair{network: network, bus: bus, a: a, b: b}
}

// newFriendsNode creates a node that uses the tagged friends bus.
func newFriendsNode(t *testing.T, network *vnet.Network, bus *friendsNet, base int, dir string, keys *fake.Keystore, tune func(*Config)) *testNode {
	t.Helper()

	disc := bus.join()
	n := newTestNodeWith(t, network, fake.NewBus(), base, dir, keys, func(cfg *Config) {
		cfg.Discovery = disc
		if tune != nil {
			tune(cfg)
		}
	})
	disc.node = n
	return n
}

// pairFriends stores the other device as a Friends peer and shares the
// rendezvous key of the pair.
func pairFriends(t *testing.T, n *testNode, other *testNode) {
	t.Helper()

	if err := n.AddPeer(context.Background(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     other.cfg.Name,
		Platform: model.PlatformMacOS,
		Role:     model.RoleFriends,
	}); err != nil {
		t.Fatalf("add friend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.Trust().SetRendezvousKey(ctx, other.DeviceID(), pairKey(n, other)); err != nil {
		t.Fatalf("set rendezvous key: %v", err)
	}
}

// pairKey returns the rendezvous key of two devices. Pairing derives it from the
// session, a test derives it from both device IDs.
func pairKey(a, b *testNode) []byte {
	return trust.DerivedRendezvousKey(a.DeviceID(), b.DeviceID())
}

// useFriends enables the friends discovery and makes the test wait for it.
func useFriends(t *testing.T, nodes ...*testNode) {
	t.Helper()

	for _, n := range nodes {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		if _, err := n.SetFriendsDiscovery(ctx, true); err != nil {
			cancel()
			t.Fatalf("enable friends discovery: %v", err)
		}
		cancel()
	}
}

// TestFriendsDiscoveryFindsEachOther checks that two Friends devices with the
// search on find each other and open a session without any address of each
// other.
func TestFriendsDiscoveryFindsEachOther(t *testing.T) {
	pair := newFriendsPair(t, 41000)
	useFriends(t, pair.a, pair.b)

	waitOnline(t, pair.a, pair.b.DeviceID())
	waitOnline(t, pair.b, pair.a.DeviceID())
	if count := pair.bus.friendsAnnouncements(); count != 2 {
		t.Fatalf("%d friends announcements after the search was turned on, want 2", count)
	}
}

// TestFriendsDiscoveryIsNotUsedWithoutMode checks that paired Friends devices
// connect to nobody while the search is off on both sides.
func TestFriendsDiscoveryIsNotUsedWithoutMode(t *testing.T) {
	pair := newFriendsPair(t, 41100)

	time.Sleep(500 * time.Millisecond)
	if pair.a.online(pair.b.DeviceID()) || pair.b.online(pair.a.DeviceID()) {
		t.Fatal("the friends devices connected without the search")
	}
	if count := pair.bus.friendsAnnouncements(); count != 0 {
		t.Fatalf("%d friends announcements without the search", count)
	}
}

// TestFriendsDiscoveryNeedsBothSides checks that the search on one side alone
// opens no session.
func TestFriendsDiscoveryNeedsBothSides(t *testing.T) {
	pair := newFriendsPair(t, 41200)
	useFriends(t, pair.a)

	time.Sleep(time.Second)
	if pair.a.online(pair.b.DeviceID()) || pair.b.online(pair.a.DeviceID()) {
		t.Fatal("the friends devices connected although the search is on one side only")
	}
}

// TestFriendsDiscoveryOffStopsDiscovery checks that turning the search off on
// one side withdraws the announcement, refuses new sessions and keeps the live
// one.
func TestFriendsDiscoveryOffStopsDiscovery(t *testing.T) {
	pair := newFriendsPair(t, 41300)
	useFriends(t, pair.a, pair.b)
	waitOnline(t, pair.a, pair.b.DeviceID())
	waitOnline(t, pair.b, pair.a.DeviceID())

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := pair.b.SetFriendsDiscovery(ctx, false); err != nil {
		t.Fatalf("turn the search off: %v", err)
	}

	if on, _, err := pair.b.FriendsDiscovery(ctx); err != nil || on {
		t.Fatalf("the search is still on (%v)", err)
	}
	deadline := time.Now().Add(testTimeout)
	for pair.bus.friendsAnnouncements() > 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if count := pair.bus.friendsAnnouncements(); count != 1 {
		t.Fatalf("%d friends announcements after the search was turned off, want 1", count)
	}
	// The live session stays.
	time.Sleep(200 * time.Millisecond)
	if pair.a.online(pair.b.DeviceID()) != pair.b.online(pair.a.DeviceID()) {
		t.Fatal("the live session ended on one side only")
	}

	// A new session is refused: the side without the search does not accept
	// the Friends device.
	if l := pair.a.lookup(pair.b.DeviceID()); l != nil {
		l.close()
	}
	if l := pair.b.lookup(pair.a.DeviceID()); l != nil {
		l.close()
	}
	waitOffline(t, pair.a, pair.b.DeviceID())
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pair.a.online(pair.b.DeviceID()) {
			t.Fatal("the device connected again after the search was turned off")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestFriendsDiscoverySurvivesStop checks that a stopped node announces
// nothing.
func TestFriendsDiscoverySurvivesStop(t *testing.T) {
	pair := newFriendsPair(t, 41400)
	useFriends(t, pair.a, pair.b)
	waitOnline(t, pair.a, pair.b.DeviceID())

	if err := pair.b.Stop(ctxOf(t)); err != nil {
		t.Fatalf("stop the node: %v", err)
	}
	if count := pair.bus.friendsAnnouncements(); count != 1 {
		t.Fatalf("%d friends announcements after a node stopped, want 1", count)
	}
}

// TestFriendsDiscoveryExpires checks that the search ends by itself and leaves
// the live session alone.
func TestFriendsDiscoveryExpires(t *testing.T) {
	clock := fake.NewClock(time.Now())
	tune := func(cfg *Config) { cfg.Clock = clock }

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := &friendsNet{}

	a := newFriendsNode(t, network, bus, 41500, t.TempDir(), fake.NewKeystore(), tune)
	b := newFriendsNode(t, network, bus, 41600, t.TempDir(), fake.NewKeystore(), tune)
	a.start(t)
	b.start(t)
	pairFriends(t, a, b)
	pairFriends(t, b, a)
	useFriends(t, a, b)
	waitOnline(t, a, b.DeviceID())

	clock.Advance(FriendsDiscoveryWindow + time.Minute)
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		on, _, err := a.FriendsDiscovery(ctxOf(t))
		if err != nil {
			t.Fatalf("read the search: %v", err)
		}
		if !on {
			if !a.online(b.DeviceID()) {
				t.Fatal("the search ended and took the live session with it")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the search did not end after the window")
}

// TestFriendsDiscoverySurvivesRestart checks that the search is read from the
// settings when the node starts again.
func TestFriendsDiscoverySurvivesRestart(t *testing.T) {
	clock := fake.NewClock(time.Now())
	tune := func(cfg *Config) { cfg.Clock = clock }

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := &friendsNet{}
	dir := t.TempDir()
	keys := fake.NewKeystore()

	first := newFriendsNode(t, network, bus, 41700, dir, keys, tune)
	first.start(t)
	useFriends(t, first)
	if err := first.Stop(ctxOf(t)); err != nil {
		t.Fatalf("stop the node: %v", err)
	}

	second := newFriendsNode(t, network, bus, 41700, dir, keys, tune)
	second.start(t)
	on, until, err := second.FriendsDiscovery(ctxOf(t))
	if err != nil {
		t.Fatalf("read the search: %v", err)
	}
	if !on {
		t.Fatal("the search did not survive the restart")
	}
	if until.Before(clock.Now()) {
		t.Fatalf("the search ends at %v, before the restart at %v", until, clock.Now())
	}
}

// TestFriendsDiscoveryRefusesStranger checks that a device without the
// rendezvous key finds nothing and is refused by the transport.
func TestFriendsDiscoveryRefusesStranger(t *testing.T) {
	pair := newFriendsPair(t, 41800)
	useFriends(t, pair.a, pair.b)
	waitOnline(t, pair.a, pair.b.DeviceID())

	network := pair.network
	stranger := newFriendsNode(t, network, pair.bus, 41900, t.TempDir(), fake.NewKeystore(), nil)
	stranger.start(t)
	useFriends(t, stranger)

	// The stranger has no relationship with either device, so no tag matches.
	if stranger.online(pair.a.DeviceID()) {
		t.Fatal("the stranger opened a session")
	}
	if count := pair.bus.friendsAnnouncements(); count != 2 {
		t.Fatalf("%d friends announcements, want the two devices and the stranger without a tag: %d", count, count)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := stranger.dial(ctx, pair.a.listenAddr())
	if err == nil {
		_ = conn.Close()
		t.Fatal("the transport accepted a device without the rendezvous key")
	}
	if !errors.Is(err, transport.ErrPeerRejected) {
		t.Fatalf("the stranger was refused with %v, want a handshake refusal", err)
	}
}
