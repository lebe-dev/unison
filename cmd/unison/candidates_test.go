package main

import (
	"slices"
	"testing"
)

// TestGroupCandidates checks that the addresses of one device become one entry,
// including the IPv4, IPv6 and zone variants of the same listener.
func TestGroupCandidates(t *testing.T) {
	addrs := []string{
		"[fe80::1c2d%en0]:54321",
		"127.0.0.1:54321",
		"[::1]:54321",
		"192.168.1.7:54321",
		"192.168.1.7:54321",
		"127.0.0.1:54322",
		"relay:fedcba9876543210",
		"relay:0123456789abcdef",
		"relay:0123456789abcdef",
		"relay:bad",
		"not-an-address",
		"127.0.0.1:0",
	}
	want := []pairingCandidate{
		{Port: 54321, Addrs: []string{
			"192.168.1.7:54321",
			"[fe80::1c2d%en0]:54321",
			"127.0.0.1:54321",
			"[::1]:54321",
		}},
		{Port: 54322, Addrs: []string{"127.0.0.1:54322"}},
		{Relay: true, Addrs: []string{"relay:0123456789abcdef"}},
		{Relay: true, Addrs: []string{"relay:fedcba9876543210"}},
	}
	got := groupCandidates(addrs)
	if len(got) != len(want) {
		t.Fatalf("grouped into %d devices, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Port != want[i].Port || got[i].Relay != want[i].Relay {
			t.Errorf("device %d has port=%d relay=%v, want port=%d relay=%v",
				i+1, got[i].Port, got[i].Relay, want[i].Port, want[i].Relay)
		}
		if !slices.Equal(got[i].Addrs, want[i].Addrs) {
			t.Errorf("device %d has addresses %v, want %v", i+1, got[i].Addrs, want[i].Addrs)
		}
	}
}

// TestGroupCandidatesStable checks that the order of the devices does not depend
// on the order discovery reports them: the numbers of the list are what the user
// types.
func TestGroupCandidatesStable(t *testing.T) {
	forward := groupCandidates([]string{"127.0.0.1:5002", "127.0.0.1:5001", "[::1]:5002"})
	backward := groupCandidates([]string{"[::1]:5002", "127.0.0.1:5001", "127.0.0.1:5002"})
	if len(forward) != 2 || len(backward) != 2 {
		t.Fatalf("grouped into %d and %d devices, want 2", len(forward), len(backward))
	}
	for i := range forward {
		if forward[i].Port != backward[i].Port || !slices.Equal(forward[i].Addrs, backward[i].Addrs) {
			t.Fatalf("the order depends on the input: %v and %v", forward, backward)
		}
	}
}

// TestChooseCandidate checks the selection of a device and of a single address of
// one device.
func TestChooseCandidate(t *testing.T) {
	candidates := []pairingCandidate{
		{Port: 54321, Addrs: []string{"192.168.1.7:54321", "127.0.0.1:54321"}},
		{Port: 54322, Addrs: []string{"192.168.1.7:54322"}},
		{Relay: true, Addrs: []string{"relay:0123456789abcdef"}},
	}
	cases := []struct {
		line string
		want string
		ok   bool
	}{
		{"", "", true},
		{"1", "192.168.1.7:54321", true},
		{" 2 ", "192.168.1.7:54322", true},
		{"3", "relay:0123456789abcdef", true},
		{"3.1", "relay:0123456789abcdef", true},
		{"1.2", "127.0.0.1:54321", true},
		{"1.3", "", false},
		{"2.2", "", false},
		{"0", "", false},
		{"4", "", false},
		{"1.x", "", false},
		{"x", "", false},
	}
	for _, c := range cases {
		got, ok := chooseCandidate(c.line, candidates)
		if got != c.want || ok != c.ok {
			t.Errorf("%q selected %q (%v), want %q (%v)", c.line, got, ok, c.want, c.ok)
		}
	}
}
