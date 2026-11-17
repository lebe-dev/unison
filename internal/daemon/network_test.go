package daemon

import (
	"context"
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
		watchAddresses(ctx, 2*time.Millisecond, 10*time.Millisecond, fingerprint, func() {
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
		watchAddresses(ctx, time.Millisecond, time.Millisecond, script.fingerprint, func() {})
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
