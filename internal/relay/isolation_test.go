package relay

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// isolationTransports runs the same isolation checks on both transports.
var isolationTransports = map[string]func(*testing.T, *Server, string, ...mailboxID) *testClient{
	"quic": dialQUIC,
	"ws":   dialWS,
}

// TestTokenIsolationSameMailboxID keeps two groups apart that use the same
// mailbox id. A connection of one token neither reaches nor observes the
// mailbox of another token.
func TestTokenIsolationSameMailboxID(t *testing.T) {
	for name, dial := range isolationTransports {
		t.Run(name, func(t *testing.T) {
			server := startServer(t, twoTokenConfig(t))
			box := newMailboxID("shared")

			first := dial(t, server, testToken, box)
			second := dial(t, server, otherToken, box)
			first.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
			second.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)

			if err := first.send(box, []byte("of the first token")); err != nil {
				t.Fatalf("send from the first token: %v", err)
			}
			second.expectNothing()
			waitFor(t, "the datagram finds no peer in its own token", func() bool {
				return server.metrics.drops[dropNoPeer].Load() == 1
			})

			if err := second.send(box, []byte("of the second token")); err != nil {
				t.Fatalf("send from the second token: %v", err)
			}
			first.expectNothing()
			waitFor(t, "the second datagram finds no peer as well", func() bool {
				return server.metrics.drops[dropNoPeer].Load() == 2
			})

			if got := server.metrics.mailboxes.Load(); got != 2 {
				t.Fatalf("mailboxes of two tokens with one id: got %d, want 2", got)
			}
		})
	}
}

// TestTokenIsolationKeepsPeersApart relays inside a token and never across
// tokens, even when every connection joins the same mailbox id.
func TestTokenIsolationKeepsPeersApart(t *testing.T) {
	server := startServer(t, twoTokenConfig(t))
	box := newMailboxID("shared")

	first := dialQUIC(t, server, testToken, box)
	peer := dialWS(t, server, testToken, box)
	foreign := dialWS(t, server, otherToken, box)

	payload := []byte("inside the first token")
	if err := first.send(box, payload); err != nil {
		t.Fatalf("send from the first token: %v", err)
	}
	peer.expectDatagram(box, payload)
	foreign.expectNothing()

	foreignPayload := []byte("inside the second token")
	if err := foreign.send(box, foreignPayload); err != nil {
		t.Fatalf("send from the second token: %v", err)
	}
	first.expectNothing()
	peer.expectNothing()
	waitFor(t, "the second token has no peer in its namespace", func() bool {
		return server.metrics.drops[dropNoPeer].Load() == 1
	})
}

// TestTokenIsolationOnUpdate moves a connection of one token onto a mailbox id
// that another token already holds. The update succeeds in its own namespace
// and the two groups still exchange nothing.
func TestTokenIsolationOnUpdate(t *testing.T) {
	server := startServer(t, twoTokenConfig(t))
	held := newMailboxID("held by the first token")
	own := newMailboxID("own of the second token")

	first := dialQUIC(t, server, testToken, held)
	second := dialWS(t, server, otherToken, own)

	if err := second.update(1, held); err != nil {
		t.Fatalf("update: %v", err)
	}
	second.expectAnswer(1, relayv1.RelayStatus_RELAY_STATUS_OK)

	if err := first.send(held, []byte("first")); err != nil {
		t.Fatalf("send from the first token: %v", err)
	}
	second.expectNothing()
	if err := second.send(held, []byte("second")); err != nil {
		t.Fatalf("send from the second token: %v", err)
	}
	first.expectNothing()

	waitFor(t, "each token is alone in its namespace", func() bool {
		return server.metrics.drops[dropNoPeer].Load() == 2
	})
}

// TestTokenIsolationUnjoinedMailbox refuses a datagram of a token that did not
// join the mailbox, even when a connection of another token holds it.
func TestTokenIsolationUnjoinedMailbox(t *testing.T) {
	server := startServer(t, twoTokenConfig(t))
	held := newMailboxID("held by the first token")
	own := newMailboxID("own of the second token")

	first := dialQUIC(t, server, testToken, held)
	second := dialWS(t, server, otherToken, own)

	if err := second.send(held, []byte("spoofed")); err != nil {
		t.Fatalf("send: %v", err)
	}
	first.expectNothing()
	waitFor(t, "the foreign mailbox is not a membership", func() bool {
		return server.metrics.drops[dropNotMember].Load() == 1
	})
}

// TestTokenIsolationEviction evicts only inside the token. A full mailbox of
// one token leaves the same mailbox id of another token untouched.
func TestTokenIsolationEviction(t *testing.T) {
	server := startServer(t, twoTokenConfig(t))
	box := newMailboxID("crowded")

	members := make([]*testClient, 0, maxMailboxMembers)
	for range maxMailboxMembers {
		members = append(members, dialQUIC(t, server, testToken, box))
	}
	oldest := members[0]

	first := dialWS(t, server, otherToken, box)
	newest := dialQUIC(t, server, testToken, box)

	// The fifth member of the first token pushes the oldest one out.
	if err := newest.send(box, []byte("inside the first token")); err != nil {
		t.Fatalf("send from the first token: %v", err)
	}
	members[len(members)-1].expectDatagram(box, []byte("inside the first token"))
	oldest.expectNothing()
	first.expectNothing()

	second := dialWS(t, server, otherToken, box)
	payload := []byte("the second token still has both peers")
	if err := first.send(box, payload); err != nil {
		t.Fatalf("send from the second token: %v", err)
	}
	second.expectDatagram(box, payload)
}

// TestTokenIsolationQuota keeps the quota of a token to its own connections. A
// group that runs out of traffic does not close the connections of a group that
// still has some.
func TestTokenIsolationQuota(t *testing.T) {
	cfg := twoTokenConfig(t)
	cfg.QuotaBytes = 1000
	server := startServer(t, cfg)
	box := newMailboxID("shared")

	first := dialQUIC(t, server, testToken, box)
	firstPeer := dialWS(t, server, testToken, box)
	second := dialQUIC(t, server, otherToken, box)
	secondPeer := dialWS(t, server, otherToken, box)

	payload := make([]byte, 400)
	for range 3 {
		if err := first.send(box, payload); err != nil {
			t.Fatalf("send from the first token: %v", err)
		}
	}
	first.expectClosed()
	if delivered := len(firstPeer.drain()); delivered != 2 {
		t.Fatalf("delivered datagrams of the first token: got %d, want 2", delivered)
	}

	secondPayload := []byte("of the second token")
	if err := second.send(box, secondPayload); err != nil {
		t.Fatalf("send from the second token: %v", err)
	}
	secondPeer.expectDatagram(box, secondPayload)
}

// TestTokenIsolationRate keeps the rate of a token to its own connections. The
// drops of a rate limited group do not delay a group that stays below its rate.
func TestTokenIsolationRate(t *testing.T) {
	cfg := twoTokenConfig(t)
	cfg.RatePerSecond = 1
	cfg.RateBurst = 1
	server := startServer(t, cfg)
	box := newMailboxID("shared")

	first := dialQUIC(t, server, testToken, box)
	firstPeer := dialWS(t, server, testToken, box)
	second := dialQUIC(t, server, otherToken, box)
	secondPeer := dialWS(t, server, otherToken, box)

	payload := []byte("rate")
	for range 5 {
		if err := first.send(box, payload); err != nil {
			t.Fatalf("send from the first token: %v", err)
		}
	}
	if delivered := len(firstPeer.drain()); delivered > 1 {
		t.Fatalf("delivered datagrams of the first token: got %d, want at most 1", delivered)
	}
	waitFor(t, "the first token is rate limited", func() bool {
		return server.metrics.drops[dropRateLimit].Load() >= 1
	})

	secondPayload := []byte("of the second token")
	if err := second.send(box, secondPayload); err != nil {
		t.Fatalf("send from the second token: %v", err)
	}
	secondPeer.expectDatagram(box, secondPayload)
}

// TestTokenIsolationMetrics counts one live mailbox per token, not per id.
func TestTokenIsolationMetrics(t *testing.T) {
	server := startServer(t, twoTokenConfig(t))
	box := newMailboxID("shared")

	dialQUIC(t, server, testToken, box)
	dialWS(t, server, otherToken, box)

	body := get(t, "http://"+server.Addrs().HTTP+"/metrics")
	if !strings.Contains(body, "unison_relay_mailboxes 2") {
		t.Errorf("metrics miss one mailbox per token:\n%s", body)
	}
}

// TestRegistryScopesMailboxes checks the registry on its own: the same id under
// two tokens is two mailboxes with separate members and separate eviction.
func TestRegistryScopesMailboxes(t *testing.T) {
	registry := newRegistry(10, newMetrics("test"), slog.New(slog.DiscardHandler))
	box := newMailboxID("shared")
	first := &peer{}
	firstPeer := &peer{}
	foreign := &peer{}

	tenantFirst := newTokenState("first", DefaultConfig(), time.Now()).tenant
	tenantForeign := newTokenState("foreign", DefaultConfig(), time.Now()).tenant

	for _, p := range []*peer{first, firstPeer} {
		if err := registry.join(p, tenantFirst, []mailboxID{box}); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	if err := registry.join(foreign, tenantForeign, []mailboxID{box}); err != nil {
		t.Fatalf("join: %v", err)
	}

	if got := registry.targets(tenantFirst, box, first); len(got) != 1 || got[0] != firstPeer {
		t.Fatalf("targets of the first token: got %v, want its own peer", got)
	}
	if got := registry.targets(tenantForeign, box, foreign); len(got) != 0 {
		t.Fatalf("targets of the foreign token: got %v, want none", got)
	}
	if registry.member(foreign, tenantFirst, box) {
		t.Fatal("a connection is a member of the mailbox of another token")
	}
	if !registry.member(foreign, tenantForeign, box) {
		t.Fatal("a connection is not a member of the mailbox of its own token")
	}
	if got := registry.metrics.mailboxes.Load(); got != 2 {
		t.Fatalf("mailboxes: got %d, want 2", got)
	}

	registry.leave(first)
	registry.leave(firstPeer)
	registry.leave(foreign)
	if got := registry.metrics.mailboxes.Load(); got != 0 {
		t.Fatalf("mailboxes after leaving: got %d, want 0", got)
	}
}
