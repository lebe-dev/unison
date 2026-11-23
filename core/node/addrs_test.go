package node

import (
	"slices"
	"testing"
	"time"
)

func TestReplaceAddr(t *testing.T) {
	cases := []struct {
		name  string
		addrs []string
		addr  string
		want  []string
	}{
		{
			name:  "new address goes first",
			addrs: []string{"10.0.0.2:4000"},
			addr:  "10.0.0.3:5000",
			want:  []string{"10.0.0.3:5000", "10.0.0.2:4000"},
		},
		{
			name:  "known address moves first",
			addrs: []string{"10.0.0.2:4000", "10.0.0.3:5000"},
			addr:  "10.0.0.3:5000",
			want:  []string{"10.0.0.3:5000", "10.0.0.2:4000"},
		},
		{
			// A device listens on one port per host; the other ports of the
			// host belong to an earlier run and never answer again.
			name:  "other ports of the host are dropped",
			addrs: []string{"10.0.0.2:4000", "[fe80::1%en0]:4000", "10.0.0.2:4100", "10.0.0.9:4000"},
			addr:  "10.0.0.2:5000",
			want:  []string{"10.0.0.2:5000", "[fe80::1%en0]:4000", "10.0.0.9:4000"},
		},
		{
			name:  "an address without a port is kept",
			addrs: []string{"garbage"},
			addr:  "10.0.0.2:5000",
			want:  []string{"10.0.0.2:5000", "garbage"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := replaceAddr(tc.addrs, tc.addr)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("replaceAddr(%v, %q) = %v, want %v", tc.addrs, tc.addr, got, tc.want)
			}
		})
	}
}

func TestCacheAddr(t *testing.T) {
	cases := []struct {
		name   string
		addrs  []string
		addr   string
		dialed bool
		want   []string
	}{
		{
			name:   "dialed address replaces the other ports of its host",
			addrs:  []string{"10.0.0.2:4000", "10.0.0.2:4100"},
			addr:   "10.0.0.2:5000",
			dialed: true,
			want:   []string{"10.0.0.2:5000"},
		},
		{
			name:  "incoming address of a new host is a hint",
			addrs: []string{"10.0.0.2:5000"},
			addr:  "10.0.0.3:6000",
			want:  []string{"10.0.0.2:5000", "10.0.0.3:6000"},
		},
		{
			// The dialer port of the peer changes with every run; the host is
			// already known, so it would only add an address that never answers.
			name:  "incoming address of a known host is dropped",
			addrs: []string{"10.0.0.2:5000"},
			addr:  "10.0.0.2:6000",
			want:  []string{"10.0.0.2:5000"},
		},
		{
			name:  "incoming address into an empty cache",
			addrs: nil,
			addr:  "10.0.0.2:6000",
			want:  []string{"10.0.0.2:6000"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cacheAddr(tc.addrs, tc.addr, tc.dialed)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("cacheAddr(%v, %q, %v) = %v, want %v", tc.addrs, tc.addr, tc.dialed, got, tc.want)
			}
		})
	}
}

func TestOfflineRetryMaxDelayDefaults(t *testing.T) {
	limits := Limits{}.withDefaults()
	if limits.OfflineRetryMaxDelay != DefaultReconnectMaxDelay {
		t.Fatalf("offline retry cap = %v, want %v", limits.OfflineRetryMaxDelay, DefaultReconnectMaxDelay)
	}

	// A cap below the reconnect cap would retry an offline peer more often
	// than a connected one that dropped.
	limits = Limits{ReconnectMaxDelay: time.Minute, OfflineRetryMaxDelay: time.Second}.withDefaults()
	if limits.OfflineRetryMaxDelay != time.Minute {
		t.Fatalf("offline retry cap = %v, want %v", limits.OfflineRetryMaxDelay, time.Minute)
	}

	limits = Limits{OfflineRetryMaxDelay: 5 * time.Minute}.withDefaults()
	if limits.OfflineRetryMaxDelay != 5*time.Minute {
		t.Fatalf("offline retry cap = %v, want 5m", limits.OfflineRetryMaxDelay)
	}
}
