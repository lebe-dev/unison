package daemon

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// addressSequence reports the fingerprints of a scripted network and keeps the
// last one once the script ends.
type addressSequence struct {
	values []string
	next   int
}

// fingerprint returns the next value of the script.
func (s *addressSequence) fingerprint() string {
	if s.next >= len(s.values) {
		return s.values[len(s.values)-1]
	}
	value := s.values[s.next]
	s.next++
	return value
}

// runWatcher runs watchAddresses with short delays and returns the number of
// reports after the test has watched for a while.
func runWatcher(t *testing.T, fingerprint func() string, watch time.Duration) int32 {
	t.Helper()

	var reports atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), watch)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchAddresses(ctx, 2*time.Millisecond, 10*time.Millisecond, fingerprint, func(string) {
			reports.Add(1)
		})
	}()
	<-done
	return reports.Load()
}

// TestWatchAddressesReportsOneChange checks that a settled change of the
// addresses is reported once.
func TestWatchAddressesReportsOneChange(t *testing.T) {
	script := &addressSequence{values: []string{"a", "a", "b", "b", "b", "b"}}
	if reports := runWatcher(t, script.fingerprint, 200*time.Millisecond); reports != 1 {
		t.Fatalf("%d reports for one change, want 1", reports)
	}
}

// TestWatchAddressesIgnoresUnsettledChange checks that a change which returns to
// the starting addresses is not reported.
func TestWatchAddressesIgnoresUnsettledChange(t *testing.T) {
	script := &addressSequence{values: []string{"a", "b", "a", "a", "a"}}
	if reports := runWatcher(t, script.fingerprint, 200*time.Millisecond); reports != 0 {
		t.Fatalf("%d reports for a change that settled back, want 0", reports)
	}
}

// TestWatchAddressesStopsWithContext checks that the watcher ends with its
// context.
func TestWatchAddressesStopsWithContext(t *testing.T) {
	script := &addressSequence{values: []string{"a", "a"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchAddresses(ctx, time.Millisecond, time.Millisecond, script.fingerprint, func(string) {})
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the watcher did not stop with its context")
	}
}

// TestLocalAddressesSkipLoopback checks that the fingerprint leaves the
// loopback interface out and stays the same on an unchanged network.
func TestLocalAddressesSkipLoopback(t *testing.T) {
	fingerprint := localAddresses()
	if fingerprint != localAddresses() {
		t.Fatal("the fingerprint of an unchanged network changed")
	}
	if strings.Contains(fingerprint, "127.0.0.1") || strings.Contains(fingerprint, "::1/") {
		t.Fatalf("the fingerprint %q contains a loopback address", fingerprint)
	}
}

// TestWatchAddressesReportsFingerprint checks that a report carries the settled
// addresses, so the daemon can tell a lost network from a new one.
func TestWatchAddressesReportsFingerprint(t *testing.T) {
	script := &addressSequence{values: []string{"a", "", ""}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	reported := make(chan string, 4)
	watchAddresses(ctx, 2*time.Millisecond, 10*time.Millisecond, script.fingerprint, func(fingerprint string) {
		reported <- fingerprint
	})
	close(reported)

	var got []string
	for fingerprint := range reported {
		got = append(got, fingerprint)
	}
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("reports %q for a lost network, want one empty fingerprint", got)
	}
}

// TestUsableAddr checks which addresses another device can reach.
func TestUsableAddr(t *testing.T) {
	cases := []struct {
		cidr string
		want bool
	}{
		{"192.168.1.5/24", true},
		{"10.0.0.7/8", true},
		{"2001:db8::1/64", true},
		{"127.0.0.1/8", false},
		{"::1/128", false},
		{"169.254.10.1/16", false},
		{"fe80::1/64", false},
		{"0.0.0.0/0", false},
		{"224.0.0.251/32", false},
	}
	for _, test := range cases {
		ip, ipNet, err := net.ParseCIDR(test.cidr)
		if err != nil {
			t.Fatalf("parse %s: %v", test.cidr, err)
		}
		ipNet.IP = ip
		if got := usableAddr(ipNet); got != test.want {
			t.Errorf("usableAddr(%s) = %v, want %v", test.cidr, got, test.want)
		}
	}
	if usableAddr(&net.IPAddr{IP: net.ParseIP("192.168.1.5")}) {
		t.Error("an address without a network is usable")
	}
}

// TestNetworkStateOf checks that a network without usable addresses is offline.
func TestNetworkStateOf(t *testing.T) {
	if networkStateOf("").Online {
		t.Error("a network without addresses is online")
	}
	if !networkStateOf("en0:192.168.1.5/24|").Online {
		t.Error("a network with an address is offline")
	}
}
