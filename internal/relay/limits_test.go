package relay

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// TestQuotaExceeded closes the connection of a token that used its quota.
func TestQuotaExceeded(t *testing.T) {
	cfg := testConfig(t)
	cfg.QuotaBytes = 2000
	server := startServer(t, cfg)
	box := newMailboxID("pair")

	sender := dialQUIC(t, server, testToken, box)
	receiver := dialWS(t, server, testToken, box)

	payload := bytes.Repeat([]byte{0x7f}, 900)
	for i := range 3 {
		if err := sender.send(box, payload); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	if delivered := len(receiver.drain(200 * time.Millisecond)); delivered != 2 {
		t.Fatalf("delivered datagrams: got %d, want 2", delivered)
	}
	sender.expectClosed()
	waitFor(t, "the connection is gone", func() bool {
		return server.metrics.connections[transportQUIC].Load() == 0
	})
}

// TestQuotaSharedByToken counts the traffic of every connection of a token.
func TestQuotaSharedByToken(t *testing.T) {
	cfg := testConfig(t)
	cfg.QuotaBytes = 1000
	server := startServer(t, cfg)
	box := newMailboxID("pair")

	first := dialQUIC(t, server, testToken, box)
	second := dialQUIC(t, server, testToken, box)
	receiver := dialWS(t, server, testToken, box)

	payload := bytes.Repeat([]byte{0x11}, 400)
	if err := first.send(box, payload); err != nil {
		t.Fatalf("send from the first: %v", err)
	}
	if err := second.send(box, payload); err != nil {
		t.Fatalf("send from the second: %v", err)
	}
	receiver.expectDatagram(box, payload)
	receiver.expectDatagram(box, payload)

	if err := second.send(box, payload); err != nil {
		t.Fatalf("send over the quota: %v", err)
	}
	second.expectClosed()
}

// TestRateLimit drops the datagrams above the rate of a token.
func TestRateLimit(t *testing.T) {
	cfg := testConfig(t)
	cfg.RatePerSecond = 1
	cfg.RateBurst = 2
	server := startServer(t, cfg)
	box := newMailboxID("pair")

	sender := dialQUIC(t, server, testToken, box)
	receiver := dialWS(t, server, testToken, box)

	payload := []byte("rate limited")
	for range 5 {
		if err := sender.send(box, payload); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	if delivered := len(receiver.drain(200 * time.Millisecond)); delivered > 2 {
		t.Fatalf("delivered datagrams: got %d, want at most 2", delivered)
	}
	if dropped := server.metrics.drops[dropRateLimit].Load(); dropped < 1 {
		t.Fatalf("rate limited datagrams: got %d, want at least 1", dropped)
	}
}

// TestOversizedDatagram drops a payload above the limit.
func TestOversizedDatagram(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxDatagramBytes = 64
	server := startServer(t, cfg)
	box := newMailboxID("pair")

	sender := dialQUIC(t, server, testToken, box)
	receiver := dialWS(t, server, testToken, box)

	if err := sender.send(box, bytes.Repeat([]byte{0x01}, 200)); err != nil {
		t.Fatalf("send: %v", err)
	}
	receiver.expectNothing()
	if dropped := server.metrics.drops[dropTooLarge].Load(); dropped != 1 {
		t.Fatalf("oversized datagrams: got %d, want 1", dropped)
	}
}

// TestUnregisteredMailbox drops a datagram of a mailbox the client did not join.
func TestUnregisteredMailbox(t *testing.T) {
	server := startServer(t, testConfig(t))
	own := newMailboxID("own")
	foreign := newMailboxID("foreign")

	sender := dialQUIC(t, server, testToken, own)
	receiver := dialWS(t, server, testToken, foreign)

	if err := sender.send(foreign, []byte("spoofed")); err != nil {
		t.Fatalf("send: %v", err)
	}
	receiver.expectNothing()
	if dropped := server.metrics.drops[dropNotMember].Load(); dropped != 1 {
		t.Fatalf("datagrams of a foreign mailbox: got %d, want 1", dropped)
	}
}

// TestInvalidMailboxID refuses mailbox ids of the wrong size.
func TestInvalidMailboxID(t *testing.T) {
	server := startServer(t, testConfig(t))

	client := dialQUICIDs(t, server, testToken, [][]byte{[]byte("short")})
	client.expectStatus(relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)

	wsClient := dialWSIDs(t, server, testToken, [][]byte{bytes.Repeat([]byte{0x02}, 20)})
	wsClient.expectStatus(relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)

	empty := dialQUICIDs(t, server, testToken, nil)
	empty.expectStatus(relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)
}

// TestTooManyMailboxes refuses a registration above the mailbox limit.
func TestTooManyMailboxes(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxMailboxes = 1
	server := startServer(t, cfg)

	client := dialQUIC(t, server, testToken, newMailboxID("one"), newMailboxID("two"))
	client.expectStatus(relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)
}

// TestMailboxEviction drops the oldest member of a full mailbox.
func TestMailboxEviction(t *testing.T) {
	server := startServer(t, testConfig(t))
	box := newMailboxID("crowded")

	clients := make([]*testClient, 0, maxMailboxMembers+1)
	for range maxMailboxMembers + 1 {
		clients = append(clients, dialQUIC(t, server, testToken, box))
	}
	oldest, sender := clients[0], clients[1]

	if err := sender.send(box, []byte("newest wins")); err != nil {
		t.Fatalf("send: %v", err)
	}
	for _, client := range clients[2:] {
		client.expectDatagram(box, []byte("newest wins"))
	}
	oldest.expectNothing()
}

// TestMailboxRegistryLimit refuses a registration when the server is full.
func TestMailboxRegistryLimit(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxMailboxesTotal = 1
	server := startServer(t, cfg)

	first := dialQUIC(t, server, testToken, newMailboxID("one"))
	first.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)

	second := dialWS(t, server, testToken, newMailboxID("two"))
	second.expectStatus(relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE)
}

// TestConnectionLimit refuses connections above the limit.
func TestConnectionLimit(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxConnections = 1
	server := startServer(t, cfg)

	first := dialQUIC(t, server, testToken, newMailboxID("one"))
	first.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)

	conn := dialRawQUIC(t, server, &quic.Config{EnableDatagrams: true})
	select {
	case <-conn.Context().Done():
	case <-time.After(testTimeout):
		t.Fatal("server kept a connection above the limit")
	}
	waitFor(t, "the refused connection is counted", func() bool {
		return server.metrics.registrations[relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE].Load() == 1
	})
}

// TestFirstFrameNotRegister refuses a connection that opens with another frame.
func TestFirstFrameNotRegister(t *testing.T) {
	server := startServer(t, testConfig(t))
	conn := dialRawQUIC(t, server, &quic.Config{EnableDatagrams: true})

	box := newMailboxID("pair")
	frame := &relayv1.Frame{Body: &relayv1.Frame_Datagram{Datagram: &relayv1.Datagram{
		MailboxId: box[:],
		Payload:   []byte("before registration"),
	}}}
	reply := registerStream(t, conn, frame)
	if got := reply.GetStatus(); got != relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT {
		t.Fatalf("status: got %v, want %v", got, relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)
	}
}

// TestUnregisteredBufferBound keeps the data a connection may push before it
// registers small. The first stream holds the server in the middle of a length
// prefix, and the other streams are never read, so everything they carry would
// stay in the memory of the server.
func TestUnregisteredBufferBound(t *testing.T) {
	server := startServer(t, testConfig(t))
	conn := dialRawQUIC(t, server, &quic.Config{EnableDatagrams: true})

	first, err := conn.OpenStream()
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if _, err := first.Write([]byte{0x80}); err != nil {
		t.Fatalf("write length prefix: %v", err)
	}

	pushed := 0
	for range 8 {
		stream, err := conn.OpenStream()
		if err != nil {
			break
		}
		if err := stream.SetWriteDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
		n, _ := stream.Write(make([]byte, 1<<20))
		pushed += n
	}

	const limit = 4 * maxRegisterBytes
	if pushed > limit {
		t.Fatalf("an unregistered connection pushed %d bytes, limit %d", pushed, limit)
	}
}

// TestRegisterWithinStreamWindow still accepts a registration of the largest
// size after the windows were reduced.
func TestRegisterWithinStreamWindow(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxMailboxes = maxLimitsMailboxes
	server := startServer(t, cfg)

	boxes := make([]mailboxID, 0, maxLimitsMailboxes)
	for i := range maxLimitsMailboxes {
		boxes = append(boxes, newMailboxID(fmt.Sprintf("box-%d", i)))
	}
	client := dialQUIC(t, server, testToken, boxes...)
	client.expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
}
