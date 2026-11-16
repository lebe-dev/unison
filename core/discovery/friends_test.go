package discovery

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

func TestRecordSetCarriesSeveralTags(t *testing.T) {
	rs := recordSet{
		typ:      "_unison-friends._udp.local.",
		instance: "abcdef",
		host:     "unison-abcdef.local.",
		port:     4242,
		txt:      map[string]string{"v": "1"},
		tags:     []string{"tagoneaaaaaaa", "tagtwoaaaaaaa"},
	}

	txt := rs.txtRecord(ttlService)
	want := []string{"v=1", "tag=tagoneaaaaaaa", "tag=tagtwoaaaaaaa"}
	if !slices.Equal(txt.Txt, want) {
		t.Fatalf("TXT = %v, want %v", txt.Txt, want)
	}
	if tags := parseTags(txt.Txt); !slices.Equal(tags, rs.tags) {
		t.Fatalf("parseTags = %v, want %v", tags, rs.tags)
	}
}

func TestParseTags(t *testing.T) {
	tags := parseTags([]string{"v=1", "tag=ABCDEFGHIJKLM", "tag=lm6xtpxkblwgm", "other=x", "tag="})
	want := []string{"abcdefghijklm", "lm6xtpxkblwgm"}
	if !slices.Equal(tags, want) {
		t.Fatalf("parseTags = %v, want %v", tags, want)
	}
	if got := parseTags(nil); got != nil {
		t.Fatalf("parseTags(nil) = %v", got)
	}
}

func TestFriendsAnnouncementTags(t *testing.T) {
	first := []byte("0123456789abcdef0123456789abcdef")
	second := []byte("fedcba9876543210fedcba9876543210")
	clock := &fixedClock{at: epochTime(1000)}

	d, err := New(Options{
		Clock:       clock,
		Ifaces:      []net.Interface{{Name: "lo0"}},
		FriendsKeys: func(context.Context) ([][]byte, error) { return [][]byte{first, second}, nil },
	})
	if err != nil {
		t.Fatalf("new discovery: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop() })

	rs := d.records(context.Background(), advert{service: ports.ServiceFriends, instance: "abcdef", port: 4242})
	want := []string{Tag(first, clock.at), Tag(second, clock.at)}
	if !slices.Equal(rs.tags, want) {
		t.Fatalf("tags = %v, want %v", rs.tags, want)
	}
	if rs.txt[txtTagKey] != "" {
		t.Fatalf("the tag leaked into the TXT map: %v", rs.txt)
	}

	main := d.records(context.Background(), advert{service: ports.ServiceMain, instance: "abcdef", port: 4242})
	if len(main.tags) != 0 {
		t.Fatalf("the main service carries friends tags: %v", main.tags)
	}
	if len(d.records(context.Background(), advert{service: ports.ServicePairing, port: 1}).tags) != 0 {
		t.Fatal("the pairing service carries tags")
	}
}

func TestMatchTags(t *testing.T) {
	first := []byte("0123456789abcdef0123456789abcdef")
	second := []byte("fedcba9876543210fedcba9876543210")
	clock := &fixedClock{at: epochTime(1000)}

	d, err := New(Options{
		Clock:       clock,
		Ifaces:      []net.Interface{{Name: "lo0"}},
		Key:         func(context.Context) ([]byte, error) { return first, nil },
		FriendsKeys: func(context.Context) ([][]byte, error) { return [][]byte{second}, nil },
	})
	if err != nil {
		t.Fatalf("new discovery: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop() })

	cases := []struct {
		name    string
		service ports.Service
		tags    []string
		want    bool
	}{
		{name: "group tag on main", service: ports.ServiceMain, tags: []string{Tag(first, clock.at)}, want: true},
		{name: "friends tag on main", service: ports.ServiceMain, tags: []string{Tag(second, clock.at)}},
		{name: "friends tag on friends", service: ports.ServiceFriends, tags: []string{Tag(second, clock.at)}, want: true},
		{name: "group tag on friends", service: ports.ServiceFriends, tags: []string{Tag(first, clock.at)}},
		{name: "second of several tags", service: ports.ServiceFriends, tags: []string{"x", Tag(second, clock.at)}, want: true},
		{name: "no tag", service: ports.ServiceFriends, tags: nil},
	}
	for _, c := range cases {
		if got := d.matchTags(c.service, c.tags); got != c.want {
			t.Errorf("%s: matchTags = %v, want %v", c.name, got, c.want)
		}
	}

	clock.at = clock.at.Add(3 * EpochPeriod)
	if d.matchTags(ports.ServiceFriends, []string{Tag(second, epochTime(1000))}) {
		t.Fatal("a tag outside the epoch window matched")
	}
}

// fixedClock is a clock that the test moves.
type fixedClock struct {
	at time.Time
}

func (c *fixedClock) Now() time.Time { return c.at }

func (c *fixedClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- c.at.Add(d)
	return ch
}

// newFriendsDiscovery creates a Discovery with group and friends keys and short
// timers.
func newFriendsDiscovery(t *testing.T, clock ports.Clock, group, friends [][]byte, ifaces ...net.Interface) *Discovery {
	t.Helper()

	d, err := New(Options{
		Keys: func(context.Context) ([][]byte, error) {
			return group, nil
		},
		FriendsKeys: func(context.Context) ([][]byte, error) {
			return friends, nil
		},
		Clock:         clock,
		Ifaces:        ifaces,
		Refresh:       100 * time.Millisecond,
		QueryInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new discovery: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop() })
	return d
}

func TestDiscoverFriendsLoopback(t *testing.T) {
	iface := testIface(t)
	clock := &testClock{now: epochTime(1000)}
	ctx := context.Background()

	first := []byte("friends key of the first pair 0")
	second := []byte("friends key of the second pair ")
	strangerKey := []byte("friends key of a stranger      ")

	publisher := newFriendsDiscovery(t, clock, nil, [][]byte{first, second}, iface)
	watcher := newFriendsDiscovery(t, clock, nil, [][]byte{second}, iface)
	stranger := newFriendsDiscovery(t, clock, nil, [][]byte{strangerKey}, iface)

	const port = 4445
	if err := publisher.Advertise(ctx, ports.ServiceFriends, port, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}

	matches, err := watcher.BrowseCandidates(ctx, ports.ServiceFriends)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	others, err := stranger.BrowseCandidates(ctx, ports.ServiceFriends)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}

	mine := waitCandidate(t, matches, port)
	if !mine.Matched {
		t.Fatalf("candidate of the pair is not matched: %+v", mine)
	}
	if mine.TXT[txtTagKey] == "" {
		t.Errorf("candidate carries no tag: %+v", mine.TXT)
	}

	theirs := waitCandidate(t, others, port)
	if theirs.Matched {
		t.Fatalf("candidate of another pair is matched: %+v", theirs)
	}

	// The group tag never appears on the friends service.
	main, err := stranger.BrowseCandidates(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse main: %v", err)
	}
	select {
	case c := <-main:
		if c.TXT[txtTagKey] == Tag(first, clock.Now()) {
			t.Fatalf("a friends tag of the pair appears on the main service: %+v", c)
		}
	case <-time.After(300 * time.Millisecond):
	}
}
