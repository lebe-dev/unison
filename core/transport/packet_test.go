package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// recorder wraps a packet connection and records the largest datagram.
type recorder struct {
	net.PacketConn

	mu  sync.Mutex
	max int
}

// WriteTo sends one datagram and records its size.
func (r *recorder) WriteTo(p []byte, addr net.Addr) (int, error) {
	r.record(len(p))
	return r.PacketConn.WriteTo(p, addr)
}

// ReadFrom receives one datagram and records its size.
func (r *recorder) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := r.PacketConn.ReadFrom(p)
	if n > 0 {
		r.record(n)
	}
	return n, addr, err
}

// record keeps the largest datagram.
func (r *recorder) record(size int) {
	r.mu.Lock()
	if size > r.max {
		r.max = size
	}
	r.mu.Unlock()
}

// largest returns the largest datagram seen so far.
func (r *recorder) largest() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.max
}

// TestPacketSizeKeepsPacketsInsideTheLimit checks that a connection with a fixed
// packet size never sends a larger datagram. The relay path needs that, because
// every datagram has to fit into one relay message.
func TestPacketSizeKeepsPacketsInsideTheLimit(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	clientVer := newTestVerifier().trust(peerOf(server, model.RolePersonal))

	const packetSize = 1200
	cfg := Config{PacketSize: packetSize}

	left := &recorder{PacketConn: testPacketConn(t, network, "server:4433")}
	right := &recorder{PacketConn: testPacketConn(t, network, "client:4434")}

	listener, err := NewListener(left, server, serverVer, cfg)
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	dialer, err := NewDialer(right, client, clientVer, cfg)
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	accepted := make(chan *Conn, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err == nil {
			accepted <- conn
		}
	}()

	clientConn, err := dialer.Dial(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	var serverConn *Conn
	select {
	case serverConn = <-accepted:
		t.Cleanup(func() { _ = serverConn.Close() })
	case <-ctx.Done():
		t.Fatal("accept did not return the connection")
	}

	// Move enough data to make the connection send full packets.
	stream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	payload := make([]byte, 8<<10)
	if _, err := stream.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	incoming, err := serverConn.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("accept stream: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(incoming, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	for name, conn := range map[string]*recorder{"listener": left, "dialer": right} {
		if largest := conn.largest(); largest > packetSize {
			t.Errorf("the %s sent %d bytes, the limit is %d", name, largest, packetSize)
		}
	}
	if left.largest() == 0 || right.largest() == 0 {
		t.Fatal("no packets were recorded")
	}
	if received := clientConn.ConnectionState().TLS.PeerCertificates; len(received) == 0 {
		t.Fatal("the connection has no peer certificate")
	}
}

// TestSharedTransportServesOneSocket checks that a listener and a dialer of a
// shared transport use one packet connection and outlive their own Close.
func TestSharedTransportServesOneSocket(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	clientVer := newTestVerifier().trust(peerOf(server, model.RolePersonal))

	shared, err := NewShared(testPacketConn(t, network, "server:4433"))
	if err != nil {
		t.Fatalf("shared: %v", err)
	}
	t.Cleanup(func() { _ = shared.Close() })

	listener, err := shared.Listener(server, serverVer, Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	dialer, err := shared.Dialer(server, serverVer, Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}

	clientDialer, err := NewDialer(testPacketConn(t, network, "client:4434"), client, clientVer, Config{})
	if err != nil {
		t.Fatalf("client dialer: %v", err)
	}
	t.Cleanup(func() { _ = clientDialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	accepted := make(chan *Conn, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err == nil {
			accepted <- conn
		}
	}()

	clientConn, err := clientDialer.Dial(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	var serverConn *Conn
	select {
	case serverConn = <-accepted:
		t.Cleanup(func() { _ = serverConn.Close() })
	case <-ctx.Done():
		t.Fatal("accept did not return the connection")
	}

	// Closing the listener and the dialer leaves the transport running, so the
	// live connection still carries data.
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	if err := dialer.Close(); err != nil {
		t.Fatalf("close dialer: %v", err)
	}
	stream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("open stream after closing the listener: %v", err)
	}
	if _, err := stream.Write([]byte("still connected")); err != nil {
		t.Fatalf("write after closing the listener: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	incoming, err := serverConn.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("accept stream after closing the listener: %v", err)
	}
	buf := make([]byte, len("still connected"))
	if _, err := io.ReadFull(incoming, buf); err != nil {
		t.Fatalf("read after closing the listener: %v", err)
	}
	if err := shared.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("close shared: %v", err)
	}
}
