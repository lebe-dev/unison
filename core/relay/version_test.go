package relay

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/protocol"
	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// TestClientReadsTheRelayProtocolVersion registers on relays that announce
// another protocol version and checks the status of the client.
func TestClientReadsTheRelayProtocolVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     uint32
		wantVersion uint32
		wantNewer   bool
	}{
		{name: "same version", version: protocol.Version, wantVersion: protocol.Version},
		{name: "newer relay", version: protocol.Version + 1, wantVersion: protocol.Version + 1, wantNewer: true},
		{name: "unset version", version: 0, wantVersion: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrations, cfg := startVersionRelay(t, test.version)
			client := startClient(t, cfg)
			openMailbox(t, client, []byte("shared rendezvous key"))

			status := client.Status()
			if status.ProtocolVersion != test.wantVersion {
				t.Errorf("protocol version: got %d, want %d", status.ProtocolVersion, test.wantVersion)
			}
			if status.ProtocolNewer != test.wantNewer {
				t.Errorf("protocol newer: got %v, want %v", status.ProtocolNewer, test.wantNewer)
			}

			select {
			case reg := <-registrations:
				if got := reg.GetProtocolVersion(); got != protocol.Version {
					t.Errorf("client sent protocol %d, want %d", got, protocol.Version)
				}
			case <-time.After(testTimeout):
				t.Fatal("the relay did not receive a registration")
			}
		})
	}
}

// TestClientWarnsAboutANewerRelay checks the warning of a client that meets a
// relay of a newer protocol version.
func TestClientWarnsAboutANewerRelay(t *testing.T) {
	_, cfg := startVersionRelay(t, protocol.Version+1)
	logs := newLogCapture()
	cfg.Logger = slog.New(logs)
	client := startClient(t, cfg)
	openMailbox(t, client, []byte("shared rendezvous key"))

	logs.expect(t, "relay runs a newer protocol version, this client needs an update")
}

// logCapture collects the messages of a logger and never blocks the caller.
type logCapture struct {
	messages chan string
}

// newLogCapture returns a handler that keeps the messages of a logger.
func newLogCapture() *logCapture {
	return &logCapture{messages: make(chan string, 16)}
}

// Enabled accepts every level.
func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }

// Handle keeps one message.
func (c *logCapture) Handle(_ context.Context, record slog.Record) error {
	select {
	case c.messages <- record.Message:
	default:
	}
	return nil
}

// WithAttrs returns the handler, which ignores attributes.
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

// WithGroup returns the handler, which ignores groups.
func (c *logCapture) WithGroup(string) slog.Handler { return c }

// expect waits for one logged message.
func (c *logCapture) expect(t *testing.T, want string) {
	t.Helper()

	deadline := time.After(testTimeout)
	for {
		select {
		case got := <-c.messages:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("no log message %q", want)
		}
	}
}

// startVersionRelay serves a WebSocket relay that answers a registration with
// the given protocol version. It returns the registrations it received and a
// client configuration that reaches it over the fallback.
func startVersionRelay(t *testing.T, version uint32) (<-chan *relayv1.Register, Config) {
	t.Helper()

	registrations := make(chan *relayv1.Register, 4)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ALPN}})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		typ, data, err := conn.Read(ctx)
		if err != nil || typ != websocket.MessageBinary {
			return
		}
		frame, err := parseFrame(data, maxRegisterBytes)
		if err != nil {
			return
		}
		if reg := frame.GetRegister(); reg != nil {
			registrations <- reg
		}

		result, err := proto.Marshal(&relayv1.Frame{Body: &relayv1.Frame_RegisterResult{
			RegisterResult: &relayv1.RegisterResult{
				Status: relayv1.RelayStatus_RELAY_STATUS_OK,
				Limits: &relayv1.Limits{
					MaxMailboxes:     8,
					MaxDatagramBytes: 1400,
					RatePerSecond:    1000,
					RateBurst:        1000,
				},
				ProtocolVersion: version,
			},
		}})
		if err != nil {
			return
		}
		if err := conn.Write(ctx, websocket.MessageBinary, result); err != nil {
			return
		}
		// Keep the connection until the client closes it.
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return
			}
		}
	})
	web := httptest.NewTLSServer(handler)
	t.Cleanup(web.Close)

	// A socket that swallows the datagrams, so the client uses the fallback.
	blackhole, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind blackhole: %v", err)
	}
	t.Cleanup(func() { _ = blackhole.Close() })

	return registrations, Config{
		URL:            web.Listener.Addr().String(),
		QUICAddr:       blackhole.LocalAddr().String(),
		Token:          testToken,
		TLS:            &tls.Config{InsecureSkipVerify: true},
		DialTimeout:    300 * time.Millisecond,
		ReconnectDelay: 50 * time.Millisecond,
		ReconnectLimit: 500 * time.Millisecond,
	}
}
