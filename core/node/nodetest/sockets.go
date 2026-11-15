package nodetest

import (
	"context"
	"net"
	"strconv"
	"sync"

	"github.com/lebe-dev/unison/core/transport/vnet"
)

// portAllocator hands out the virtual addresses of one harness.
type portAllocator struct {
	mu   sync.Mutex
	next int
}

// newPortAllocator creates an allocator that starts at first.
func newPortAllocator(first int) *portAllocator {
	return &portAllocator{next: first}
}

// addr returns the next free address.
func (a *portAllocator) addr() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(a.next))
	a.next++
	return addr
}

// sockets binds the packet connections of one device and controls their link.
type sockets struct {
	network *vnet.Network
	ports   *portAllocator

	mu    sync.Mutex
	conns []*vnet.Conn
}

// newSockets creates the socket set of one device.
func newSockets(network *vnet.Network, ports *portAllocator) *sockets {
	return &sockets{network: network, ports: ports}
}

// listenPacket binds a free address. The requested port is ignored, so the
// address a device announces is the address its listener is bound to.
func (s *sockets) listenPacket(_ context.Context, _ int) (net.PacketConn, error) {
	pc, err := s.network.ListenPacket(s.ports.addr())
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns = append(s.conns, pc)
	return pc, nil
}

// listenAddr returns the address of the first bound socket, which is the main
// listener, or an empty string before it is bound.
func (s *sockets) listenAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.conns) == 0 {
		return ""
	}
	return s.conns[0].LocalAddr().String()
}

// setDown drops every datagram to and from the device.
func (s *sockets) setDown(down bool) {
	for _, pc := range s.snapshot() {
		if down {
			pc.Disconnect()
			continue
		}
		pc.Reconnect()
	}
}

// moveAll binds every socket to a new address and returns the addresses that
// the sockets left.
func (s *sockets) moveAll() ([]string, error) {
	conns := s.snapshot()

	previous := make([]string, 0, len(conns))
	for _, pc := range conns {
		previous = append(previous, pc.LocalAddr().String())
		if err := pc.MoveTo(s.ports.addr()); err != nil {
			return previous, err
		}
	}
	return previous, nil
}

// snapshot returns the bound sockets.
func (s *sockets) snapshot() []*vnet.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*vnet.Conn(nil), s.conns...)
}
