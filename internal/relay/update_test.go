package relay

import (
	"strings"
	"testing"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// updateTransports dials a client over each transport.
var updateTransports = map[string]func(*testing.T, *Server, string, ...mailboxID) *testClient{
	"quic": dialQUIC,
	"ws":   dialWS,
}

// TestUpdateReplacesMailboxes moves a registered connection to another mailbox
// set without a new connection.
func TestUpdateReplacesMailboxes(t *testing.T) {
	for name, dial := range updateTransports {
		t.Run(name, func(t *testing.T) {
			server := startServer(t, testConfig(t))
			first := newMailboxID("first")
			second := newMailboxID("second")

			client := dial(t, server, testToken, first)
			if !client.reply.GetLimits().GetMailboxUpdates() {
				t.Fatal("the relay does not announce mailbox updates")
			}
			onFirst := dialQUIC(t, server, testToken, first)
			onSecond := dialQUIC(t, server, testToken, second)

			if err := client.update(1, first, second); err != nil {
				t.Fatalf("update: %v", err)
			}
			client.expectAnswer(1, relayv1.RelayStatus_RELAY_STATUS_OK)
			if err := client.send(second, []byte("joined")); err != nil {
				t.Fatalf("send: %v", err)
			}
			onSecond.expectDatagram(second, []byte("joined"))

			if err := client.update(2, second); err != nil {
				t.Fatalf("update: %v", err)
			}
			client.expectAnswer(2, relayv1.RelayStatus_RELAY_STATUS_OK)
			if err := onFirst.send(first, []byte("left")); err != nil {
				t.Fatalf("send: %v", err)
			}
			client.expectNothing()
			if err := onSecond.send(second, []byte("still there")); err != nil {
				t.Fatalf("send: %v", err)
			}
			client.expectDatagram(second, []byte("still there"))
		})
	}
}

// TestUpdateAnswersRepeatedSequence answers a repeated update again, so a
// client whose answer was lost learns the outcome, and ignores an older one.
func TestUpdateAnswersRepeatedSequence(t *testing.T) {
	server := startServer(t, testConfig(t))
	first := newMailboxID("first")
	second := newMailboxID("second")

	client := dialQUIC(t, server, testToken, first)
	for range 2 {
		if err := client.update(5, second); err != nil {
			t.Fatalf("update: %v", err)
		}
		client.expectAnswer(5, relayv1.RelayStatus_RELAY_STATUS_OK)
	}

	// An older update arrives late: the relay keeps the newer set.
	if err := client.update(4, first); err != nil {
		t.Fatalf("update: %v", err)
	}
	client.expectNoAnswer()
	peer := dialQUIC(t, server, testToken, second)
	if err := peer.send(second, []byte("newer set")); err != nil {
		t.Fatalf("send: %v", err)
	}
	client.expectDatagram(second, []byte("newer set"))
}

// TestUpdateAboveLimitKeepsTheSet refuses an update above the mailbox limit and
// leaves the connection in its previous mailboxes.
func TestUpdateAboveLimitKeepsTheSet(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxMailboxes = 1
	server := startServer(t, cfg)
	first := newMailboxID("first")

	client := dialQUIC(t, server, testToken, first)
	if err := client.update(1, first, newMailboxID("second")); err != nil {
		t.Fatalf("update: %v", err)
	}
	client.expectAnswer(1, relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)

	peer := dialQUIC(t, server, testToken, first)
	if err := peer.send(first, []byte("kept")); err != nil {
		t.Fatalf("send: %v", err)
	}
	client.expectDatagram(first, []byte("kept"))
}

// TestUpdateOffBehavesLikeAnOlderRelay checks the switch that turns mailbox
// updates off: the relay does not announce them and ignores an update.
func TestUpdateOffBehavesLikeAnOlderRelay(t *testing.T) {
	cfg := testConfig(t)
	cfg.NoMailboxUpdates = true
	server := startServer(t, cfg)
	first := newMailboxID("first")

	client := dialQUIC(t, server, testToken, first)
	if client.reply.GetLimits().GetMailboxUpdates() {
		t.Fatal("the relay announces mailbox updates although they are off")
	}
	if err := client.update(1, newMailboxID("second")); err != nil {
		t.Fatalf("update: %v", err)
	}
	client.expectNoAnswer()
}

// TestUpdateMetrics counts the updates by outcome.
func TestUpdateMetrics(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxMailboxes = 1
	server := startServer(t, cfg)

	client := dialQUIC(t, server, testToken, newMailboxID("first"))
	if err := client.update(1, newMailboxID("second")); err != nil {
		t.Fatalf("update: %v", err)
	}
	client.expectAnswer(1, relayv1.RelayStatus_RELAY_STATUS_OK)
	if err := client.update(2, newMailboxID("one"), newMailboxID("two")); err != nil {
		t.Fatalf("update: %v", err)
	}
	client.expectAnswer(2, relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)

	body := get(t, "http://"+server.Addrs().HTTP+"/metrics")
	for _, name := range []string{
		`unison_relay_mailbox_updates_total{status="ok"} 1`,
		`unison_relay_mailbox_updates_total{status="invalid_argument"} 1`,
		"unison_relay_mailboxes 1",
	} {
		if !strings.Contains(body, name) {
			t.Errorf("metrics miss %q:\n%s", name, body)
		}
	}
}
