package relay

import (
	"net"
	"os"
	"sync"
	"time"
)

// Addr is the address of a relay packet connection. The relay carries no
// addresses of its own, so the address only names the local mailbox.
type Addr struct {
	name string
}

// Network returns the network of the address.
func (a Addr) Network() string { return "relay" }

// String returns the name of the address.
func (a Addr) String() string { return a.name }

// Conn is a virtual packet connection to one mailbox. It implements
// net.PacketConn and carries the packets of one peer connection.
type Conn struct {
	dialer *Dialer
	key    []byte
	addr   Addr
	queue  int

	// pendingRev is the mailbox set revision that has to be registered before
	// the connection is usable.
	pendingRev uint64

	closed chan struct{}
	once   sync.Once

	// signal wakes a blocked read when a datagram is queued.
	signal chan struct{}

	mu            sync.Mutex
	datagrams     [][]byte
	readDeadline  time.Time
	writeDeadline time.Time
	// deadlineCh is closed and replaced whenever the read deadline changes, so
	// a blocked read recomputes its wait.
	deadlineCh chan struct{}
}

// Conn implements net.PacketConn.
var _ net.PacketConn = (*Conn)(nil)

// newConn creates a packet connection for a pair.
func newConn(dialer *Dialer, key []byte, addr Addr, queue int) *Conn {
	return &Conn{
		dialer:     dialer,
		key:        key,
		addr:       addr,
		queue:      queue,
		closed:     make(chan struct{}),
		signal:     make(chan struct{}, 1),
		deadlineCh: make(chan struct{}),
	}
}

// LocalAddr returns the address of the connection.
func (c *Conn) LocalAddr() net.Addr {
	return c.addr
}

// RemoteAddr returns the address of the peer. The relay connects the members of
// one mailbox only, so the address stands for the peer.
func (c *Conn) RemoteAddr() net.Addr {
	return c.addr
}

// ReadFrom waits for a datagram and copies it into p. A datagram larger than p
// is truncated, like a UDP datagram read into a small buffer.
func (c *Conn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		c.mu.Lock()
		if len(c.datagrams) > 0 {
			data := c.datagrams[0]
			c.datagrams[0] = nil
			c.datagrams = c.datagrams[1:]
			c.mu.Unlock()
			return copy(p, data), c.addr, nil
		}
		deadline := c.readDeadline
		changed := c.deadlineCh
		c.mu.Unlock()

		if c.isClosed() {
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
		case <-c.closed:
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

// WriteTo sends one datagram to the mailbox. A datagram sent while the relay is
// not connected is dropped, like a packet on a broken link.
func (c *Conn) WriteTo(p []byte, _ net.Addr) (int, error) {
	if c.isClosed() {
		return 0, net.ErrClosed
	}
	if err := c.dialer.send(c, p); err != nil {
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

// Close closes the connection and leaves the mailboxes of the pair.
func (c *Conn) Close() error {
	c.once.Do(c.release)
	c.dialer.remove(c)
	return nil
}

// close ends the connection without releasing the mailboxes, which a client
// that shuts down does for every connection.
func (c *Conn) close() {
	c.once.Do(c.release)
}

// release drops the queued datagrams and wakes the blocked reads.
func (c *Conn) release() {
	close(c.closed)
	c.mu.Lock()
	c.datagrams = nil
	c.mu.Unlock()
}

// isClosed reports whether the connection is closed.
func (c *Conn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// writeExpired reports whether the write deadline has passed.
func (c *Conn) writeExpired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.writeDeadline.IsZero() && !time.Now().Before(c.writeDeadline)
}

// push queues an incoming datagram. The queue is bounded, the excess is dropped.
func (c *Conn) push(data []byte) {
	if c.isClosed() {
		return
	}

	c.mu.Lock()
	if len(c.datagrams) >= c.queue {
		c.mu.Unlock()
		return
	}
	c.datagrams = append(c.datagrams, data)
	c.mu.Unlock()

	select {
	case c.signal <- struct{}{}:
	default:
	}
}

// SetReadBuffer is part of the interface QUIC uses to size the socket buffer.
// The queue of the connection has no kernel limit, so the call does nothing.
func (c *Conn) SetReadBuffer(int) error { return nil }

// SetWriteBuffer is part of the interface QUIC uses to size the socket buffer.
// The queue of the connection has no kernel limit, so the call does nothing.
func (c *Conn) SetWriteBuffer(int) error { return nil }

// mailboxes returns the mailboxes of the connection at t.
func (c *Conn) mailboxes(t time.Time, rollover time.Duration) []ID {
	return Mailboxes(c.key, t, rollover)
}

// keyString returns a stable key of the pair.
func (c *Conn) keyString() string {
	return string(c.key)
}
