package relay

import (
	"bufio"
	"bytes"
	"log/slog"
	"testing"
	"time"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// silentConn is the connection of a peer whose answers are dropped.
type silentConn struct{}

func (silentConn) write([]byte) error              { return nil }
func (silentConn) close(relayv1.RelayStatus) error { return nil }

// FuzzServerDatagram checks that an arbitrary datagram of a registered client
// is either dropped or forwarded inside the limits.
func FuzzServerDatagram(f *testing.F) {
	seedMailbox := newMailboxID("seed")
	seed, err := marshalFrame(&relayv1.Frame{Body: &relayv1.Frame_Datagram{
		Datagram: &relayv1.Datagram{MailboxId: seedMailbox[:], Payload: []byte("payload")},
	}})
	if err != nil {
		f.Fatalf("build a datagram: %v", err)
	}
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})
	f.Add(make([]byte, mailboxIDLen))

	f.Fuzz(func(t *testing.T, data []byte) {
		server := newTestServer(t)
		client := newTestPeer(server)

		frame, err := parseFrame(data, server.cfg.MaxDatagramBytes+frameOverhead)
		if err == nil {
			if datagram := frame.GetDatagram(); datagram != nil {
				if mailbox, ok := mailboxIDOf(datagram.GetMailboxId()); ok {
					if err := server.boxes.join(client, []mailboxID{mailbox}); err != nil {
						t.Fatalf("join a mailbox: %v", err)
					}
				}
			}
		}
		server.forward(client, data)
	})
}

// FuzzServerRegister checks that an arbitrary registration frame is either
// refused or joins the mailboxes it names.
func FuzzServerRegister(f *testing.F) {
	mailbox := newMailboxID("seed")
	frame, err := marshalFrame(&relayv1.Frame{Body: &relayv1.Frame_Register{
		Register: &relayv1.Register{Token: []byte("token"), MailboxIds: [][]byte{mailbox[:]}},
	}})
	if err != nil {
		f.Fatalf("build a registration: %v", err)
	}
	var stream bytes.Buffer
	if err := writeStreamFrame(&stream, frame); err != nil {
		f.Fatalf("frame a registration: %v", err)
	}
	f.Add(stream.Bytes())
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add(make([]byte, mailboxIDLen))

	f.Fuzz(func(t *testing.T, data []byte) {
		server := newTestServer(t)
		client := newTestPeer(server)

		reader := bufio.NewReaderSize(bytes.NewReader(data), streamBufferSize)
		// A part of the inputs uses a tighter limit, so the boundary of the
		// length check is covered as well.
		limit := maxRegisterBytes
		if len(data)%4 == 0 {
			limit = maxRegisterBytes / 2
		}
		frame, err := readStreamFrame(reader, limit)
		if err != nil {
			return
		}
		registration := frame.GetRegister()
		if registration == nil {
			return
		}
		status := server.register(client, registration)
		if status != relayv1.RelayStatus_RELAY_STATUS_OK {
			return
		}
		if len(registration.GetMailboxIds()) == 0 {
			t.Fatalf("accepted a registration without mailboxes")
		}
	})
}

// newTestServer returns a server without sockets.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Logger = slog.New(slog.DiscardHandler)
	cfg.TLSCertFile = "test-cert.pem"
	cfg.TLSKeyFile = "test-key.pem"
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.tokens = newTokenSet([]string{"token"}, server.cfg, time.Now())
	return server
}

// newTestPeer returns a client of the server that drops its output.
func newTestPeer(server *Server) *peer {
	return &peer{
		srv:    server,
		writer: silentConn{},
		token:  newTokenState("token", server.cfg, time.Now()),
		out:    make(chan []byte, outQueue),
		done:   make(chan struct{}),
	}
}
