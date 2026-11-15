// Package vnet provides an in-memory packet network for connection tests. It
// routes datagrams between virtual addresses and reproduces the failures of a
// real link: packet loss, latency, outages and address changes.
package vnet

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Network is a set of virtual packet connections that exchange datagrams by
// address. A Network is safe for concurrent use.
type Network struct {
	mu      sync.Mutex
	conns   map[string]*Conn
	loss    float64
	latency time.Duration
	rng     *rand.Rand
	closed  bool

	delivered atomic.Uint64
	dropped   atomic.Uint64
}

// NewNetwork creates an empty network without loss and latency.
func NewNetwork() *Network {
	return &Network{
		conns: make(map[string]*Conn),
		rng:   rand.New(rand.NewPCG(0x756e69736f6e, 1)),
	}
}

// SetLoss sets the probability of dropping a datagram. The value must be
// between 0 and 1.
func (n *Network) SetLoss(p float64) error {
	if p < 0 || p > 1 {
		return fmt.Errorf("vnet: loss %v is outside [0, 1]", p)
	}
	n.mu.Lock()
	n.loss = p
	n.mu.Unlock()
	return nil
}

// SetLatency sets the delivery delay of every datagram.
func (n *Network) SetLatency(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("vnet: negative latency %v", d)
	}
	n.mu.Lock()
	n.latency = d
	n.mu.Unlock()
	return nil
}

// SetSeed reseeds the packet loss generator, which makes a test reproducible.
func (n *Network) SetSeed(seed uint64) {
	n.mu.Lock()
	n.rng = rand.New(rand.NewPCG(seed, seed))
	n.mu.Unlock()
}

// ListenPacket binds a new connection to addr. The address must not be in use.
// The connection is removed from the network by Conn.Close.
func (n *Network) ListenPacket(addr string) (*Conn, error) {
	if addr == "" {
		return nil, errors.New("vnet: empty address")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, net.ErrClosed
	}
	if n.conns[addr] != nil {
		return nil, fmt.Errorf("vnet: address %s is in use", addr)
	}

	c := newConn(n, addr)
	n.conns[addr] = c
	return c, nil
}

// Stats returns the number of datagrams the network delivered and the number it
// dropped.
func (n *Network) Stats() (delivered, dropped uint64) {
	return n.delivered.Load(), n.dropped.Load()
}

// Close closes every connection of the network.
func (n *Network) Close() error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return nil
	}
	n.closed = true
	conns := make([]*Conn, 0, len(n.conns))
	for _, c := range n.conns {
		conns = append(conns, c)
	}
	n.conns = make(map[string]*Conn)
	n.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}

// route delivers one datagram from the connection bound to local to the
// connection bound to remote. A datagram to an unknown address, from or to a
// disconnected connection, or one dropped by the simulated loss disappears.
func (n *Network) route(local *Conn, remote string, data []byte) error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return net.ErrClosed
	}
	dst := n.conns[remote]
	latency := n.latency
	drop := n.loss > 0 && n.rng.Float64() < n.loss
	n.mu.Unlock()

	if dst == nil || drop {
		n.dropped.Add(1)
		return nil
	}

	n.delivered.Add(1)
	from := local.addr.Load()
	packet := bytes.Clone(data)
	if latency <= 0 {
		dst.push(packet, from)
		return nil
	}
	time.AfterFunc(latency, func() { dst.push(packet, from) })
	return nil
}

// Addr is the address of a virtual connection.
type Addr struct {
	name string
}

// Addr implements net.Addr.
var _ net.Addr = (*Addr)(nil)

// NewAddr returns the virtual address with the given name, for example
// "10.0.0.2:4000".
func NewAddr(name string) *Addr {
	return &Addr{name: name}
}

// Network returns the network name of the address.
func (a *Addr) Network() string {
	return "vnet"
}

// String returns the address name.
func (a *Addr) String() string {
	return a.name
}
