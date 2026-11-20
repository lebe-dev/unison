package relay

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/protocol"
	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// TestForwardQUIC relays datagrams between two QUIC clients.
func TestForwardQUIC(t *testing.T) {
	server := startServer(t, testConfig(t))
	box := newMailboxID("pair")

	first := dialQUIC(t, server, testToken, box)
	second := dialQUIC(t, server, testToken, box)
	first.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
	second.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)

	payload := []byte("ciphertext")
	if err := first.send(box, payload); err != nil {
		t.Fatalf("send: %v", err)
	}
	second.expectDatagram(box, payload)
	first.expectNothing()

	if err := second.send(box, payload); err != nil {
		t.Fatalf("send back: %v", err)
	}
	first.expectDatagram(box, payload)
}

// TestForwardWebSocket relays datagrams between two clients of the fallback.
func TestForwardWebSocket(t *testing.T) {
	server := startServer(t, testConfig(t))
	box := newMailboxID("pair")

	first := dialWS(t, server, testToken, box)
	second := dialWS(t, server, testToken, box)
	first.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
	second.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)

	payload := []byte("ciphertext over websocket")
	if err := first.send(box, payload); err != nil {
		t.Fatalf("send: %v", err)
	}
	second.expectDatagram(box, payload)
	first.expectNothing()
}

// TestForwardMixed relays datagrams between the two transports.
func TestForwardMixed(t *testing.T) {
	server := startServer(t, testConfig(t))
	box := newMailboxID("pair")

	quicClient := dialQUIC(t, server, testToken, box)
	wsClient := dialWS(t, server, testToken, box)

	payload := []byte("mixed")
	if err := quicClient.send(box, payload); err != nil {
		t.Fatalf("send from quic: %v", err)
	}
	wsClient.expectDatagram(box, payload)

	if err := wsClient.send(box, payload); err != nil {
		t.Fatalf("send from websocket: %v", err)
	}
	quicClient.expectDatagram(box, payload)
}

// TestMailboxIsolation keeps the members of different mailboxes apart.
func TestMailboxIsolation(t *testing.T) {
	server := startServer(t, testConfig(t))
	first := newMailboxID("one")
	second := newMailboxID("two")

	sender := dialQUIC(t, server, testToken, first)
	other := dialWS(t, server, testToken, second)

	if err := sender.send(first, []byte("lonely")); err != nil {
		t.Fatalf("send: %v", err)
	}
	other.expectNothing()
	if dropped := server.metrics.drops[dropNoPeer].Load(); dropped != 1 {
		t.Fatalf("dropped without a peer: got %d, want 1", dropped)
	}
}

// TestMultipleMailboxes lets one connection join several mailboxes.
func TestMultipleMailboxes(t *testing.T) {
	server := startServer(t, testConfig(t))
	first := newMailboxID("first")
	second := newMailboxID("second")

	sender := dialQUIC(t, server, testToken, first, second)
	one := dialWS(t, server, testToken, first)
	two := dialWS(t, server, testToken, second)

	if err := sender.send(second, []byte("to the second")); err != nil {
		t.Fatalf("send: %v", err)
	}
	two.expectDatagram(second, []byte("to the second"))
	one.expectNothing()
}

// TestMetricsEndpoint reports the counters of the server.
func TestMetricsEndpoint(t *testing.T) {
	server := startServer(t, testConfig(t))
	box := newMailboxID("metrics")

	sender := dialQUIC(t, server, testToken, box)
	receiver := dialQUIC(t, server, testToken, box)
	payload := []byte("counted")
	if err := sender.send(box, payload); err != nil {
		t.Fatalf("send: %v", err)
	}
	receiver.expectDatagram(box, payload)

	body := get(t, "http://"+server.Addrs().HTTP+"/metrics")
	for _, name := range []string{
		"unison_relay_connections{transport=\"quic\"} 2",
		"unison_relay_connections{transport=\"ws\"} 0",
		"unison_relay_mailboxes 1",
		`unison_relay_datagrams_total{transport="quic",direction="in"} 1`,
		"unison_relay_registrations_total{status=\"ok\"} 2",
	} {
		if !strings.Contains(body, name) {
			t.Errorf("metrics miss %q:\n%s", name, body)
		}
	}
	if strings.Contains(body, hex.EncodeToString(box[:])) {
		t.Error("metrics carry a mailbox id")
	}
	if strings.Contains(body, "127.0.0.1") {
		t.Error("metrics carry an address")
	}

	for _, path := range []string{"/healthz", "/readyz"} {
		if code := status(t, "http://"+server.Addrs().HTTP+path); code != http.StatusOK {
			t.Errorf("%s: got %d, want %d", path, code, http.StatusOK)
		}
	}
}

// TestUnknownToken refuses a registration with a token the server does not know.
func TestUnknownToken(t *testing.T) {
	cfg := testConfig(t)
	cfg.Tokens = []string{testToken}
	// A wrong token that has the same length as the valid one.
	server := startServer(t, cfg)
	box := newMailboxID("pair")

	quicClient := dialQUIC(t, server, "test-tokeX", box)
	quicClient.expectStatus(relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED)
	quicClient.expectClosed()

	wsClient := dialWS(t, server, "", box)
	wsClient.expectStatus(relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED)
	wsClient.expectClosed()

	if got := server.metrics.registrations[relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED].Load(); got != 2 {
		t.Fatalf("unauthorized registrations: got %d, want 2", got)
	}
	if got := server.metrics.mailboxes.Load(); got != 0 {
		t.Fatalf("mailboxes after rejected registrations: got %d, want 0", got)
	}
}

// TestDatagramsRequired refuses a QUIC client without datagram support.
func TestDatagramsRequired(t *testing.T) {
	server := startServer(t, testConfig(t))

	conn := dialRawQUIC(t, server, &quic.Config{})

	select {
	case <-conn.Context().Done():
	case <-time.After(testTimeout):
		t.Fatal("server kept a connection without datagram support")
	}
	if got := server.metrics.registrations[relayv1.RelayStatus_RELAY_STATUS_DATAGRAMS_REQUIRED].Load(); got != 1 {
		t.Fatalf("datagram rejections: got %d, want 1", got)
	}
}

// TestShutdown closes the client connections with the server.
func TestShutdown(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	box := newMailboxID("pair")
	client := dialQUIC(t, server, testToken, box)

	if err := server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	client.expectClosed()
	if err := server.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// TestRegisterWithoutProtocolVersion accepts a client that predates the version
// field, which reads as version 1.
func TestRegisterWithoutProtocolVersion(t *testing.T) {
	server := startServer(t, testConfig(t))
	box := newMailboxID("old client")

	for name, client := range map[string]*testClient{
		"quic": dialQUIC(t, server, testToken, box),
		"ws":   dialWS(t, server, testToken, box),
	} {
		client.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
		if got := client.reply.GetProtocolVersion(); got != protocol.Version {
			t.Errorf("%s: relay answered protocol %d, want %d", name, got, protocol.Version)
		}
	}
}

// TestRegisterWithNewerProtocolVersion accepts a client of a newer protocol and
// answers with the version of the relay.
func TestRegisterWithNewerProtocolVersion(t *testing.T) {
	server := startServer(t, testConfig(t))
	box := newMailboxID("new client")

	conn := dialRawQUIC(t, server, &quic.Config{EnableDatagrams: true})
	reply := registerStream(t, conn, &relayv1.Frame{Body: &relayv1.Frame_Register{
		Register: &relayv1.Register{
			Token:           []byte(testToken),
			MailboxIds:      wireMailboxes([]mailboxID{box}),
			ProtocolVersion: protocol.Version + 1,
		},
	}})
	if got := reply.GetStatus(); got != relayv1.RelayStatus_RELAY_STATUS_OK {
		t.Fatalf("registration status: got %v, want ok", got)
	}
	if got := reply.GetProtocolVersion(); got != protocol.Version {
		t.Fatalf("relay answered protocol %d, want %d", got, protocol.Version)
	}
}

// TestRegisterLogsANewerClient checks the warning about a client of a newer
// protocol version.
func TestRegisterLogsANewerClient(t *testing.T) {
	cfg := testConfig(t)
	logs := newMessageLog()
	cfg.Logger = slog.New(logs)
	server := startServer(t, cfg)
	box := newMailboxID("new client")

	conn := dialRawQUIC(t, server, &quic.Config{EnableDatagrams: true})
	reply := registerStream(t, conn, &relayv1.Frame{Body: &relayv1.Frame_Register{
		Register: &relayv1.Register{
			Token:           []byte(testToken),
			MailboxIds:      wireMailboxes([]mailboxID{box}),
			ProtocolVersion: protocol.Version + 1,
		},
	}})
	if got := reply.GetStatus(); got != relayv1.RelayStatus_RELAY_STATUS_OK {
		t.Fatalf("registration status: got %v, want ok", got)
	}
	logs.expect(t, "client runs a newer protocol version")
}

// messageLog collects the messages of a logger and never blocks the caller.
type messageLog struct {
	messages chan string
}

// newMessageLog returns a handler that keeps the messages of a logger.
func newMessageLog() *messageLog {
	return &messageLog{messages: make(chan string, 16)}
}

// Enabled accepts every level.
func (m *messageLog) Enabled(context.Context, slog.Level) bool { return true }

// Handle keeps one message.
func (m *messageLog) Handle(_ context.Context, record slog.Record) error {
	select {
	case m.messages <- record.Message:
	default:
	}
	return nil
}

// WithAttrs returns the handler, which ignores attributes.
func (m *messageLog) WithAttrs([]slog.Attr) slog.Handler { return m }

// WithGroup returns the handler, which ignores groups.
func (m *messageLog) WithGroup(string) slog.Handler { return m }

// expect waits for one logged message.
func (m *messageLog) expect(t *testing.T, want string) {
	t.Helper()

	deadline := time.After(testTimeout)
	for {
		select {
		case got := <-m.messages:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("no log message %q", want)
		}
	}
}

// expectStatus checks the answer to the registration.
func (c *testClient) expectStatus(want relayv1.RelayStatus) {
	c.t.Helper()

	if c.reply == nil {
		c.t.Fatal("no registration result")
	}
	if got := c.reply.GetStatus(); got != want {
		c.t.Fatalf("registration status: got %v, want %v", got, want)
	}
}

// expectDatagram waits for a datagram of the mailbox.
func (c *testClient) expectDatagram(box mailboxID, payload []byte) {
	c.t.Helper()

	datagram, err := c.recv()
	if err != nil {
		c.t.Fatalf("receive: %v", err)
	}
	if !bytes.Equal(datagram.GetMailboxId(), box[:]) {
		c.t.Fatalf("mailbox id: got %x, want %x", datagram.GetMailboxId(), box[:])
	}
	if !bytes.Equal(datagram.GetPayload(), payload) {
		c.t.Fatalf("payload: got %q, want %q", datagram.GetPayload(), payload)
	}
}

// get fetches a URL and returns the body.
func get(t *testing.T, url string) string {
	t.Helper()

	resp, err := httpGet(t, url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(body)
}

// status fetches a URL and returns the status code.
func status(t *testing.T, url string) int {
	t.Helper()

	resp, err := httpGet(t, url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	return resp.StatusCode
}
