package relay

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports/fake"
	relayserver "github.com/lebe-dev/unison/internal/relay"
)

// opened returns the connections the relay accepted so far, over both
// transports.
func (r *testRelay) opened(t *testing.T) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+r.server.Addrs().HTTP+"/metrics", nil)
	if err != nil {
		t.Fatalf("metrics request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	total := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "unison_relay_connections_total{") {
			continue
		}
		fields := strings.Fields(line)
		value, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			t.Fatalf("parse metric %q: %v", line, err)
		}
		total += value
	}
	return total
}

// exchange sends one payload each way between two connections of a pair.
func exchange(t *testing.T, a, b *Conn, payload string) {
	t.Helper()

	write(t, a, payload)
	if got := read(t, b); got != payload {
		t.Fatalf("the peer received %q, want %q", got, payload)
	}
	write(t, b, payload+" back")
	if got := read(t, a); got != payload+" back" {
		t.Fatalf("the peer received %q, want %q", got, payload+" back")
	}
}

// blockedQUIC returns a client configuration whose QUIC attempts get no answer,
// so the client uses WebSocket.
func blockedQUIC(t *testing.T, server *testRelay) Config {
	t.Helper()

	blackhole, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind blackhole: %v", err)
	}
	t.Cleanup(func() { _ = blackhole.Close() })
	cfg := server.client(t)
	cfg.QUICAddr = blackhole.LocalAddr().String()
	return cfg
}

// TestClientChangesMailboxesWithoutReconnect opens and closes the mailboxes of
// further pairs on a live connection: the relay connection stays the same, so
// the traffic of the other pairs is not interrupted.
func TestClientChangesMailboxesWithoutReconnect(t *testing.T) {
	for _, transport := range []string{"quic", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			server := startTestRelay(t)
			cfg := server.client(t)
			if transport == "websocket" {
				cfg = blockedQUIC(t, server)
			}
			first := startClient(t, cfg)
			second := startClient(t, server.client(t))

			firstConn := openMailbox(t, first, []byte("first pair"))
			secondConn := openMailbox(t, second, []byte("first pair"))
			exchange(t, firstConn, secondConn, "first pair")
			before := server.opened(t)

			firstOther, err := first.Open(context.Background(), []byte("second pair"))
			if err != nil {
				t.Fatalf("open mailbox: %v", err)
			}
			secondOther := openMailbox(t, second, []byte("second pair"))
			exchange(t, firstOther, secondOther, "second pair")
			exchange(t, firstConn, secondConn, "first pair again")

			_ = firstOther.Close()
			_ = secondOther.Close()
			exchange(t, firstConn, secondConn, "first pair after the close")

			if after := server.opened(t); after != before {
				t.Fatalf("the relay accepted %d connections while the mailboxes changed", after-before)
			}
		})
	}
}

// TestClientRollsOverWithoutReconnect crosses UTC midnight: the clients move to
// the mailboxes of the new day on their live connections.
func TestClientRollsOverWithoutReconnect(t *testing.T) {
	server := startTestRelay(t)
	clock := fake.NewClock(time.Date(2026, time.November, 16, 23, 0, 0, 0, time.UTC))

	cfg := server.client(t)
	cfg.Clock = clock
	first := startClient(t, cfg)
	second := startClient(t, cfg)

	key := []byte("shared rendezvous key")
	firstConn := openMailbox(t, first, key)
	secondConn := openMailbox(t, second, key)
	exchange(t, firstConn, secondConn, "before the rollover window")
	before := server.opened(t)

	// The rollover window starts, then midnight passes, then the window ends.
	for _, step := range []time.Duration{40 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		clock.Advance(step)
		waitExchange(t, firstConn, secondConn, "after "+step.String())
	}

	if after := server.opened(t); after != before {
		t.Fatalf("the relay accepted %d connections across midnight", after-before)
	}
}

// waitExchange retries a payload until the peer receives it, since both
// clients move to the mailboxes of a new day at their own pace.
func waitExchange(t *testing.T, a, b *Conn, payload string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		write(t, a, payload)
		if err := b.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
		n, _, err := b.ReadFrom(buf)
		if err == nil && string(buf[:n]) == payload {
			_ = b.SetReadDeadline(time.Time{})
			return
		}
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("read datagram: %v", err)
		}
	}
	t.Fatalf("%q did not reach the peer within %v", payload, testTimeout)
}

// TestClientReconnectsOnOlderRelay adds a mailbox on a relay without mailbox
// updates: the client registers the new set on a new connection.
func TestClientReconnectsOnOlderRelay(t *testing.T) {
	server := startTestRelayWith(t, func(cfg *relayserver.Config) { cfg.NoMailboxUpdates = true })
	first := startClient(t, server.client(t))
	second := startClient(t, server.client(t))

	firstConn := openMailbox(t, first, []byte("first pair"))
	secondConn := openMailbox(t, second, []byte("first pair"))
	exchange(t, firstConn, secondConn, "first pair")

	firstOther := openMailbox(t, first, []byte("second pair"))
	secondOther := openMailbox(t, second, []byte("second pair"))
	exchange(t, firstOther, secondOther, "second pair")
	waitExchange(t, firstConn, secondConn, "first pair again")
}

// TestClientMailboxLimit opens more mailboxes than the relay allows on one
// connection: the pair above the limit is refused, the earlier pairs keep
// theirs, and the refused pair gets a mailbox once another one closes.
func TestClientMailboxLimit(t *testing.T) {
	server := startTestRelayWith(t, func(cfg *relayserver.Config) { cfg.MaxMailboxes = 2 })
	first := startClient(t, server.client(t))
	second := startClient(t, server.client(t))

	firstA := openMailbox(t, first, []byte("pair a"))
	secondA := openMailbox(t, second, []byte("pair a"))
	firstB, err := first.Open(context.Background(), []byte("pair b"))
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	secondB := openMailbox(t, second, []byte("pair b"))

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := first.Open(ctx, []byte("pair c")); !errors.Is(err, ErrMailboxLimit) {
		t.Fatalf("open above the limit: %v, want %v", err, ErrMailboxLimit)
	}
	exchange(t, firstA, secondA, "pair a keeps its mailbox")

	_ = firstB.Close()
	_ = secondB.Close()
	firstC := openMailbox(t, first, []byte("pair c"))
	secondC := openMailbox(t, second, []byte("pair c"))
	waitExchange(t, firstC, secondC, "pair c after the close")
}
