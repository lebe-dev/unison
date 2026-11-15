package vnet

import (
	"bytes"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// TestDeliver checks that a datagram reaches the peer address with its sender.
func TestDeliver(t *testing.T) {
	network := NewNetwork()
	defer func() { _ = network.Close() }()

	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen a: %v", err)
	}
	b, err := network.ListenPacket("b:1")
	if err != nil {
		t.Fatalf("listen b: %v", err)
	}

	if _, err := a.WriteTo([]byte("hello"), b.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 16)
	n, from, err := b.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got, want := string(buf[:n]), "hello"; got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
	if from.String() != "a:1" {
		t.Fatalf("from = %s, want a:1", from)
	}
}

// TestUnknownAddressDrops checks that a datagram to an address without a
// connection disappears instead of failing the sender.
func TestUnknownAddressDrops(t *testing.T) {
	network := NewNetwork()
	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	n, err := a.WriteTo([]byte("lost"), NewAddr("nowhere:1"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 4 {
		t.Fatalf("written = %d, want 4", n)
	}

	if err := a.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	buf := make([]byte, 8)
	if _, _, err := a.ReadFrom(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read error = %v, want deadline exceeded", err)
	}
}

// TestLatency checks that delivery waits for the configured delay.
func TestLatency(t *testing.T) {
	network := NewNetwork()
	if err := network.SetLatency(30 * time.Millisecond); err != nil {
		t.Fatalf("latency: %v", err)
	}
	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b, err := network.ListenPacket("b:1")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	start := time.Now()
	if _, err := a.WriteTo([]byte("delayed"), b.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := b.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, _, err := b.ReadFrom(make([]byte, 16)); err != nil {
		t.Fatalf("read: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("delivered after %v, want at least 30ms", elapsed)
	}
}

// TestLoss checks that the loss rate drops the expected share of datagrams.
func TestLoss(t *testing.T) {
	network := NewNetwork()
	if err := network.SetLoss(0.5); err != nil {
		t.Fatalf("loss: %v", err)
	}
	network.SetSeed(1)

	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen a: %v", err)
	}
	b, err := network.ListenPacket("b:1")
	if err != nil {
		t.Fatalf("listen b: %v", err)
	}

	const sent = 400
	for range sent {
		if _, err := a.WriteTo([]byte("x"), b.LocalAddr()); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	received := 0
	for {
		if err := b.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
			t.Fatalf("deadline: %v", err)
		}
		if _, _, err := b.ReadFrom(make([]byte, 8)); err != nil {
			break
		}
		received++
	}
	if received < sent/4 || received > sent*3/4 {
		t.Fatalf("received %d of %d datagrams, want roughly half", received, sent)
	}
}

// TestDisconnect checks that an outage drops both directions and that a
// reconnect resumes delivery.
func TestDisconnect(t *testing.T) {
	network := NewNetwork()
	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen a: %v", err)
	}
	b, err := network.ListenPacket("b:1")
	if err != nil {
		t.Fatalf("listen b: %v", err)
	}

	a.Disconnect()
	if _, err := a.WriteTo([]byte("lost"), b.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := b.WriteTo([]byte("lost"), a.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := a.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, _, err := a.ReadFrom(make([]byte, 8)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read error = %v, want deadline exceeded", err)
	}

	a.Reconnect()
	if _, err := b.WriteTo([]byte("back"), a.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := a.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	buf := make([]byte, 8)
	n, _, err := a.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf[:n], []byte("back")) {
		t.Fatalf("payload = %q, want back", buf[:n])
	}
}

// TestMoveTo checks that a connection keeps working under a new address.
func TestMoveTo(t *testing.T) {
	network := NewNetwork()
	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen a: %v", err)
	}
	b, err := network.ListenPacket("b:1")
	if err != nil {
		t.Fatalf("listen b: %v", err)
	}

	if err := a.MoveTo("a:2"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if got := a.LocalAddr().String(); got != "a:2" {
		t.Fatalf("address = %s, want a:2", got)
	}
	if _, err := network.ListenPacket("a:2"); err == nil {
		t.Fatal("bound the address the moved connection holds")
	}

	if _, err := a.WriteTo([]byte("moved"), b.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := b.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	buf := make([]byte, 8)
	n, from, err := b.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if from.String() != "a:2" {
		t.Fatalf("from = %s, want a:2", from)
	}
	if !bytes.Equal(buf[:n], []byte("moved")) {
		t.Fatalf("payload = %q, want moved", buf[:n])
	}
}

// TestClose checks that a closed connection rejects reads and writes and frees
// its address.
func TestClose(t *testing.T) {
	network := NewNetwork()
	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, _, err := a.ReadFrom(make([]byte, 8)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("read error = %v, want net.ErrClosed", err)
	}
	if _, err := a.WriteTo([]byte("x"), NewAddr("b:1")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write error = %v, want net.ErrClosed", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if _, err := network.ListenPacket("a:1"); err != nil {
		t.Fatalf("listen on freed address: %v", err)
	}
}

// TestReadDeadlineChange checks that a blocked read wakes when the deadline
// moves closer.
func TestReadDeadlineChange(t *testing.T) {
	network := NewNetwork()
	a, err := network.ListenPacket("a:1")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := a.ReadFrom(make([]byte, 8))
		done <- err
	}()

	time.Sleep(10 * time.Millisecond)
	if err := a.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("read error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not wake up after the deadline change")
	}
}
