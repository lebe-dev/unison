package discovery

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/lebe-dev/unison/core/ports"
)

// testClock is a Clock advanced by the test.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testIface returns an interface that carries multicast. The loopback
// interface is preferred so the test does not depend on a physical network.
func testIface(t *testing.T) net.Interface {
	t.Helper()

	if iface, err := net.InterfaceByName("lo0"); err == nil && multicast(iface) {
		return *iface
	}
	all, err := net.Interfaces()
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	for _, iface := range all {
		if multicast(&iface) && iface.Flags&net.FlagLoopback == 0 {
			return iface
		}
	}
	t.Skip("no multicast interface")
	return net.Interface{}
}

func multicast(iface *net.Interface) bool {
	return iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagMulticast != 0
}

// newTestDiscovery creates a Discovery with short timers.
func newTestDiscovery(t *testing.T, clock ports.Clock, key []byte, ifaces ...net.Interface) *Discovery {
	t.Helper()

	d, err := New(Options{
		Key: func(context.Context) ([]byte, error) {
			return key, nil
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

// waitCandidate waits for a candidate on the given port.
func waitCandidate(t *testing.T, ch <-chan Candidate, port int) Candidate {
	t.Helper()

	deadline := time.After(20 * time.Second)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				t.Fatalf("browse channel closed before a candidate on port %d", port)
			}
			_, gotPort, err := net.SplitHostPort(c.Addr)
			if err != nil {
				continue
			}
			if gotPort == strconv.Itoa(port) {
				return c
			}
		case <-deadline:
			t.Fatalf("no candidate on port %d", port)
		}
	}
}

func TestDiscoverLoopback(t *testing.T) {
	iface := testIface(t)
	clock := &testClock{now: epochTime(1000)}
	ctx := context.Background()

	publisher := newTestDiscovery(t, clock, testKey, iface)
	watcher := newTestDiscovery(t, clock, testKey, iface)
	stranger := newTestDiscovery(t, clock, []byte("the other group key 0123456789"), iface)

	const port = 4242
	if err := publisher.Advertise(ctx, ports.ServiceMain, port, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}

	matches, err := watcher.BrowseCandidates(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	others, err := stranger.BrowseCandidates(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}

	mine := waitCandidate(t, matches, port)
	if !mine.Matched {
		t.Fatalf("candidate of the same group is not matched: %+v", mine)
	}
	wantTag := Tag(testKey, clock.Now())
	if mine.TXT["tag"] != wantTag {
		t.Errorf("candidate tag = %q, want %q", mine.TXT["tag"], wantTag)
	}
	if mine.TXT["v"] != txtVersion {
		t.Errorf("candidate version = %q, want %q", mine.TXT["v"], txtVersion)
	}

	theirs := waitCandidate(t, others, port)
	if theirs.Matched {
		t.Fatalf("candidate of another group is matched: %+v", theirs)
	}
	if theirs.TXT["tag"] != wantTag {
		t.Errorf("candidate tag = %q, want %q", theirs.TXT["tag"], wantTag)
	}
}

func TestPairingService(t *testing.T) {
	iface := testIface(t)
	clock := &testClock{now: epochTime(1000)}
	ctx := context.Background()

	pairer := newTestDiscovery(t, clock, testKey, iface)
	browser := newTestDiscovery(t, clock, testKey, iface)

	const port = 4343
	if err := pairer.Advertise(ctx, ports.ServicePairing, port, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}
	mainCh, err := browser.BrowseCandidates(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse main: %v", err)
	}
	pairingCh, err := browser.BrowseCandidates(ctx, ports.ServicePairing)
	if err != nil {
		t.Fatalf("browse pairing: %v", err)
	}

	c := waitCandidate(t, pairingCh, port)
	if c.Matched {
		t.Errorf("pairing candidate is matched: %+v", c)
	}
	if _, ok := c.TXT[txtTagKey]; ok {
		t.Errorf("pairing candidate carries a tag: %+v", c)
	}
	if c.TXT[txtVersionKey] != txtVersion {
		t.Errorf("pairing candidate version = %q, want %q", c.TXT[txtVersionKey], txtVersion)
	}

	select {
	case c := <-mainCh:
		t.Fatalf("main browse delivered pairing candidate %+v", c)
	case <-time.After(500 * time.Millisecond):
	}

	if err := pairer.StopService(ports.ServicePairing); err != nil {
		t.Fatalf("stop pairing: %v", err)
	}
	if err := pairer.StopService(ports.ServicePairing); err != nil {
		t.Fatalf("stop pairing again: %v", err)
	}
}

func TestReannounceOnEpochChange(t *testing.T) {
	iface := testIface(t)
	clock := &testClock{now: epochTime(1000)}
	ctx := context.Background()

	publisher := newTestDiscovery(t, clock, testKey, iface)
	watcher := newTestDiscovery(t, clock, testKey, iface)

	const port = 4444
	if err := publisher.Advertise(ctx, ports.ServiceMain, port, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}
	ch, err := watcher.BrowseCandidates(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}

	first := waitCandidate(t, ch, port)
	if !first.Matched {
		t.Fatalf("candidate is not matched: %+v", first)
	}

	clock.advance(EpochPeriod)

	deadline := time.After(20 * time.Second)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				t.Fatal("browse channel closed")
			}
			if c.Addr != first.Addr || c.TXT["tag"] == first.TXT["tag"] {
				continue
			}
			if !c.Matched {
				t.Fatalf("rotated candidate is not matched: %+v", c)
			}
			if want := Tag(testKey, clock.Now()); c.TXT["tag"] != want {
				t.Fatalf("rotated tag = %q, want %q", c.TXT["tag"], want)
			}
			return
		case <-deadline:
			t.Fatalf("no announcement with a new tag after the epoch change, first was %q", first.TXT["tag"])
		}
	}
}

func TestStop(t *testing.T) {
	iface := testIface(t)
	clock := &testClock{now: epochTime(1000)}
	ctx := context.Background()

	d := newTestDiscovery(t, clock, testKey, iface)
	if err := d.Advertise(ctx, ports.ServiceMain, 4455, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}
	ch, err := d.BrowseCandidates(ctx, ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("stop again: %v", err)
	}

	drained := make(chan struct{})
	go func() {
		for range ch {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("browse channel was not closed by Stop")
	}

	if err := d.Advertise(ctx, ports.ServiceMain, 4455, nil); !errors.Is(err, ErrStopped) {
		t.Fatalf("advertise after stop = %v, want %v", err, ErrStopped)
	}
	if _, err := d.BrowseCandidates(ctx, ports.ServiceMain); !errors.Is(err, ErrStopped) {
		t.Fatalf("browse after stop = %v, want %v", err, ErrStopped)
	}
}

func TestAnswer(t *testing.T) {
	clock := &testClock{now: epochTime(1000)}
	d := &Discovery{
		clock: clock,
		host:  "unison-abcdef.local.",
		ads:   make(map[ports.Service]*advert),
		opts: Options{
			Key: func(context.Context) ([]byte, error) { return testKey, nil },
		},
	}
	ad := advert{service: ports.ServiceMain, instance: "abcdef", port: 4242}
	typ, err := serviceType(ports.ServiceMain)
	if err != nil {
		t.Fatal(err)
	}

	q := query(typ)
	resp := d.answer([]advert{ad}, packet{msg: q})
	if resp == nil {
		t.Fatal("no response to a service type query")
	}
	if len(resp.Answer) != 1 || resp.Answer[0].Header().Rrtype != dns.TypePTR {
		t.Fatalf("answer = %v, want one PTR record", resp.Answer)
	}
	if got := resp.Answer[0].(*dns.PTR).Ptr; got != "abcdef."+typ {
		t.Errorf("PTR = %q, want the instance name", got)
	}
	if len(resp.Extra) < 2 {
		t.Fatalf("extra = %v, want SRV and TXT records", resp.Extra)
	}

	q = &dns.Msg{Question: []dns.Question{{Name: "abcdef." + typ, Qtype: dns.TypeANY, Qclass: dns.ClassINET}}}
	resp = d.answer([]advert{ad}, packet{msg: q})
	if resp == nil {
		t.Fatal("no response to an instance query")
	}
	var txt *dns.TXT
	for _, rr := range resp.Answer {
		if rr, ok := rr.(*dns.TXT); ok {
			txt = rr
		}
	}
	if txt == nil {
		t.Fatal("no TXT record in the instance answer")
	}
	want := Tag(testKey, clock.Now())
	if fields := parseTXT(txt.Txt); fields[txtTagKey] != want {
		t.Errorf("tag = %q, want %q", fields[txtTagKey], want)
	}

	if resp := d.answer([]advert{ad}, packet{msg: query("other._unison._udp.local.")}); resp != nil {
		t.Fatalf("answered an unrelated query: %v", resp.Answer)
	}

	q = query(typ)
	q.Id = 1234
	from := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5354}
	resp = d.answer([]advert{ad}, packet{msg: q, from: from})
	if resp == nil || resp.Id != 1234 {
		t.Fatalf("legacy answer id = %v, want the query id", resp)
	}
}
