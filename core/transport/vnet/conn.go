package vnet

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// maxQueue bounds the datagrams waiting to be read by one connection. The
// packets beyond the limit are dropped, like a link with a full buffer.
const maxQueue = 512

// Conn is a virtual packet connection. It implements net.PacketConn.
type Conn struct {
	net  *Network
	addr atomic.Pointer[Addr]
	down atomic.Bool
	gone atomic.Bool

	// closedCh is closed when the connection is closed.
	closedCh chan struct{}

	// signal wakes a blocked read when a datagram is queued.
	signal chan struct{}

	mu            sync.Mutex
	queue         []datagram
	readDeadline  time.Time
	writeDeadline time.Time
	// deadlineCh is closed and replaced whenever the read deadline changes, so
	// a blocked read recomputes its wait.
	deadlineCh chan struct{}
}

// Conn implements net.PacketConn.
var _ net.PacketConn = (*Conn)(nil)

// datagram is one queued packet and the virtual address that sent it.
type datagram struct {
	data []byte
	from *Addr
}

// newConn creates a connection bound to addr. The caller registers it in the
// network.
func newConn(n *Network, addr string) *Conn {
	c := &Conn{
		net:        n,
		closedCh:   make(chan struct{}),
		signal:     make(chan struct{}, 1),
		deadlineCh: make(chan struct{}),
	}
	c.addr.Store(NewAddr(addr))
	return c
}

// LocalAddr returns the address the connection is currently bound to.
func (c *Conn) LocalAddr() net.Addr {
	return c.addr.Load()
}

// ReadFrom waits for a datagram and copies it into p. A datagram larger than p
// is truncated, like a UDP datagram read into a small buffer.
func (c *Conn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			d := c.queue[0]
			c.queue[0] = datagram{}
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return copy(p, d.data), d.from, nil
		}
		deadline := c.readDeadline
		changed := c.deadlineCh
		c.mu.Unlock()

		if c.gone.Load() {
			return 0, nil, net.ErrClosed
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return 0, nil, os.ErrDeadlineExceeded
		}

		var expiry <-chan time.Time
		var timer *time.Timer
		if !deadline.IsZero() {
			timer = time.NewTimer(time.Until(deadline))
			expiry = timer.C
		}

		select {
		case <-c.signal:
		case <-c.closedCh:
		case <-changed:
		case <-expiry:
			timer.Stop()
			return 0, nil, os.ErrDeadlineExceeded
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// WriteTo sends one datagram to the connection bound to addr. A datagram to an
// unknown address, and a datagram of a disconnected connection, is dropped
// silently, like a packet that never reaches its destination.
func (c *Conn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if addr == nil {
		return 0, errors.New("vnet: nil address")
	}
	if c.gone.Load() {
		return 0, net.ErrClosed
	}
	if c.writeExpired() {
		return 0, os.ErrDeadlineExceeded
	}
	if c.down.Load() {
		return len(p), nil
	}
	if err := c.net.route(c, addr.String(), p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// SetDeadline sets the read and the write deadline.
func (c *Conn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// SetReadDeadline sets the time after which a blocked read returns
// os.ErrDeadlineExceeded. The zero value removes the deadline.
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	close(c.deadlineCh)
	c.deadlineCh = make(chan struct{})
	c.mu.Unlock()
	return nil
}

// SetWriteDeadline sets the time after which a write fails with
// os.ErrDeadlineExceeded. The zero value removes the deadline.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.writeDeadline = t
	c.mu.Unlock()
	return nil
}

// Close closes the connection and unregisters it from the network.
func (c *Conn) Close() error {
	if !c.gone.CompareAndSwap(false, true) {
		return nil
	}
	close(c.closedCh)

	c.mu.Lock()
	c.queue = nil
	c.mu.Unlock()

	addr := c.addr.Load().String()
	c.net.mu.Lock()
	if c.net.conns[addr] == c {
		delete(c.net.conns, addr)
	}
	c.net.mu.Unlock()
	return nil
}

// Disconnect drops every datagram to and from the connection, which simulates a
// link outage. The connection keeps its address and can be brought back with
// Reconnect.
func (c *Conn) Disconnect() {
	c.down.Store(true)
	c.mu.Lock()
	c.queue = nil
	c.mu.Unlock()
}

// Reconnect ends the outage started by Disconnect.
func (c *Conn) Reconnect() {
	c.down.Store(false)
}

// MoveTo binds the connection to another address, which simulates a device that
// changed its network.
func (c *Conn) MoveTo(addr string) error {
	if addr == "" {
		return errors.New("vnet: empty address")
	}
	if c.gone.Load() {
		return net.ErrClosed
	}

	c.net.mu.Lock()
	defer c.net.mu.Unlock()
	if c.net.closed {
		return net.ErrClosed
	}
	if other := c.net.conns[addr]; other != nil && other != c {
		return fmt.Errorf("vnet: address %s is in use", addr)
	}
	delete(c.net.conns, c.addr.Load().String())
	c.addr.Store(NewAddr(addr))
	c.net.conns[addr] = c
	return nil
}

// push queues an incoming datagram. The queue is bounded, the excess is dropped.
func (c *Conn) push(data []byte, from *Addr) {
	if c.gone.Load() || c.down.Load() {
		return
	}

	c.mu.Lock()
	if c.gone.Load() || c.down.Load() || len(c.queue) >= maxQueue {
		c.mu.Unlock()
		return
	}
	c.queue = append(c.queue, datagram{data: data, from: from})
	c.mu.Unlock()

	select {
	case c.signal <- struct{}{}:
	default:
	}
}

// SetReadBuffer is part of the interface quic-go uses to size the socket
// buffer. The memory queue of the connection has no kernel limit, so the call
// does nothing.
func (c *Conn) SetReadBuffer(int) error {
	return nil
}

// SetWriteBuffer is part of the interface quic-go uses to size the socket
// buffer. The memory queue of the connection has no kernel limit, so the call
// does nothing.
func (c *Conn) SetWriteBuffer(int) error {
	return nil
}

// writeExpired reports whether the write deadline has passed.
func (c *Conn) writeExpired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.writeDeadline.IsZero() && !time.Now().Before(c.writeDeadline)
}
