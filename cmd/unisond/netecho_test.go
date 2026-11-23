package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// TestNetEchoAnswersProbe checks the round trip of the probe.
func TestNetEchoAnswersProbe(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan int, 1)
	go func() { done <- serveNetEcho(context.Background(), conn, 0, io.Discard) }()

	code := probeNetEcho(context.Background(), conn.LocalAddr().String(), 2*time.Second, io.Discard)
	if code != exitOK {
		t.Errorf("probe exited with %d, want %d", code, exitOK)
	}

	_ = conn.Close()
	<-done
}

// TestNetEchoTimesOutWithoutProbe checks that a bounded echo fails when no
// probe arrives.
func TestNetEchoTimesOutWithoutProbe(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = conn.Close() }()

	code := serveNetEcho(context.Background(), conn, 100*time.Millisecond, io.Discard)
	if code != exitError {
		t.Errorf("serve exited with %d, want %d", code, exitError)
	}
}

// TestNetEchoNetwork checks the socket family of a listen address.
func TestNetEchoNetwork(t *testing.T) {
	cases := []struct {
		address string
		want    string
	}{
		{"0.0.0.0:47811", "udp4"},
		{"127.0.0.1:47811", "udp4"},
		{"[::]:47811", "udp6"},
		{":47811", "udp"},
		{"example.com:47811", "udp"},
	}
	for _, tc := range cases {
		if got := netEchoNetwork(tc.address); got != tc.want {
			t.Errorf("netEchoNetwork(%q) = %q, want %q", tc.address, got, tc.want)
		}
	}
}

// TestNetEchoProbeWithoutServer checks that a missing peer is a failure.
func TestNetEchoProbeWithoutServer(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := conn.LocalAddr().String()
	_ = conn.Close()

	code := probeNetEcho(context.Background(), address, 300*time.Millisecond, io.Discard)
	if code != exitError {
		t.Errorf("probe exited with %d, want %d", code, exitError)
	}
}
