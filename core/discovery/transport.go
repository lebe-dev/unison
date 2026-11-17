package discovery

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/lebe-dev/unison/internal/observability"
)

const (
	// mdnsPort is the port every mDNS node listens on.
	mdnsPort = 5353
	// packetBuffer is the number of received messages buffered per transport.
	packetBuffer = 32
	// maxPacketSize is the largest mDNS message accepted from the network.
	maxPacketSize = 9000
)

var (
	group4 = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: mdnsPort}
	group6 = &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: mdnsPort}
)

// packet is a received mDNS message.
type packet struct {
	msg   *dns.Msg
	iface *net.Interface
	from  *net.UDPAddr
}

// transport is a pair of mDNS sockets joined to the multicast groups of the
// configured interfaces.
type transport struct {
	ifaces []net.Interface
	v4     *ipv4.PacketConn
	v6     *ipv6.PacketConn

	packets   chan packet
	done      chan struct{}
	closeOnce sync.Once
	sendMu    sync.Mutex
	log       *slog.Logger
	// access learns from every send whether the local network is reachable.
	access *lanAccess
}

// openTransport opens the mDNS sockets and joins the groups on every
// interface. An address family without a usable interface is disabled.
func openTransport(ifaces []net.Interface, logger *slog.Logger, access *lanAccess) (*transport, error) {
	t := &transport{
		ifaces:  ifaces,
		packets: make(chan packet, packetBuffer),
		done:    make(chan struct{}),
		log:     logger,
		access:  access,
	}
	t.v4 = join4(ifaces, logger)
	t.v6 = join6(ifaces, logger)
	if t.v4 == nil && t.v6 == nil {
		return nil, errors.New("discovery: no mDNS socket")
	}

	if t.v4 != nil {
		go t.read4()
	}
	if t.v6 != nil {
		go t.read6()
	}
	return t, nil
}

func join4(ifaces []net.Interface, logger *slog.Logger) *ipv4.PacketConn {
	conn, err := net.ListenUDP("udp4", group4)
	if err != nil {
		conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: mdnsPort})
		if err != nil {
			debugf(logger, "open mDNS socket for IPv4: %v", err)
			return nil
		}
	}

	p := ipv4.NewPacketConn(conn)
	_ = p.SetControlMessage(ipv4.FlagInterface, true)
	_ = p.SetMulticastLoopback(true)
	_ = p.SetMulticastTTL(255)
	_ = p.SetTTL(255)

	joined := 0
	for i := range ifaces {
		if err := p.JoinGroup(&ifaces[i], group4); err != nil {
			debugf(logger, "join IPv4 mDNS group on %s: %v", ifaces[i].Name, err)
			continue
		}
		joined++
	}
	if joined == 0 {
		_ = p.Close()
		return nil
	}
	return p
}

func join6(ifaces []net.Interface, logger *slog.Logger) *ipv6.PacketConn {
	conn, err := net.ListenUDP("udp6", group6)
	if err != nil {
		conn, err = net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6unspecified, Port: mdnsPort})
		if err != nil {
			debugf(logger, "open mDNS socket for IPv6: %v", err)
			return nil
		}
	}

	p := ipv6.NewPacketConn(conn)
	_ = p.SetControlMessage(ipv6.FlagInterface, true)
	_ = p.SetMulticastLoopback(true)
	_ = p.SetMulticastHopLimit(255)
	_ = p.SetHopLimit(255)

	joined := 0
	for i := range ifaces {
		if err := p.JoinGroup(&ifaces[i], group6); err != nil {
			debugf(logger, "join IPv6 mDNS group on %s: %v", ifaces[i].Name, err)
			continue
		}
		joined++
	}
	if joined == 0 {
		_ = p.Close()
		return nil
	}
	return p
}

// read4 receives IPv4 mDNS messages until the socket is closed.
func (t *transport) read4() {
	defer observability.Recover()
	buf := make([]byte, maxPacketSize)
	for {
		n, cm, from, err := t.v4.ReadFrom(buf)
		if err != nil {
			return
		}
		index := 0
		if cm != nil {
			index = cm.IfIndex
		}
		t.dispatch(buf[:n], index, from)
	}
}

// read6 receives IPv6 mDNS messages until the socket is closed.
func (t *transport) read6() {
	defer observability.Recover()
	buf := make([]byte, maxPacketSize)
	for {
		n, cm, from, err := t.v6.ReadFrom(buf)
		if err != nil {
			return
		}
		index := 0
		if cm != nil {
			index = cm.IfIndex
		}
		t.dispatch(buf[:n], index, from)
	}
}

// dispatch parses a message and hands it to the reader of the transport. Any
// host of the local network can send one, so a panic of the parser drops the
// message instead of stopping the process.
func (t *transport) dispatch(data []byte, ifIndex int, from net.Addr) {
	defer func() {
		if recovered := recover(); recovered != nil {
			debugf(t.log, "dropped an mDNS message that broke the parser: %v", recovered)
		}
	}()
	msg := new(dns.Msg)
	if err := msg.Unpack(data); err != nil {
		return
	}
	addr, _ := from.(*net.UDPAddr)

	p := packet{msg: msg, iface: t.iface(ifIndex), from: addr}
	select {
	case t.packets <- p:
	case <-t.done:
	default:
	}
}

// iface returns the interface with index. Messages from an interface outside
// the group are reported with a nil interface.
func (t *transport) iface(index int) *net.Interface {
	for i := range t.ifaces {
		if t.ifaces[i].Index == index {
			return &t.ifaces[i]
		}
	}
	if index == 0 {
		return nil
	}
	iface, err := net.InterfaceByIndex(index)
	if err != nil {
		return nil
	}
	return iface
}

// send writes msg to the multicast groups on iface.
func (t *transport) send(iface *net.Interface, msg *dns.Msg) error {
	if iface == nil {
		return errors.New("discovery: unknown interface")
	}
	buf, err := msg.Pack()
	if err != nil {
		return fmt.Errorf("discovery: pack mDNS message: %w", err)
	}

	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	var errs []error
	if t.v4 != nil {
		if err := t.v4.SetMulticastInterface(iface); err != nil {
			errs = append(errs, err)
		} else if _, err := t.v4.WriteTo(buf, nil, group4); err != nil {
			t.access.record(iface, err)
			errs = append(errs, err)
		} else {
			t.access.record(iface, nil)
		}
	}
	if t.v6 != nil {
		if err := t.v6.SetMulticastInterface(iface); err != nil {
			errs = append(errs, err)
		} else if _, err := t.v6.WriteTo(buf, nil, group6); err != nil {
			t.access.record(iface, err)
			errs = append(errs, err)
		} else {
			t.access.record(iface, nil)
		}
	}
	return errors.Join(errs...)
}

// reply answers a request. Requests from the mDNS port get a multicast
// response, simple resolvers get a unicast one.
func (t *transport) reply(iface *net.Interface, from *net.UDPAddr, msg *dns.Msg) error {
	if from == nil || from.Port == mdnsPort {
		return t.send(iface, msg)
	}

	buf, err := msg.Pack()
	if err != nil {
		return fmt.Errorf("discovery: pack mDNS message: %w", err)
	}

	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	switch {
	case from.IP.To4() != nil && t.v4 != nil:
		_, err = t.v4.WriteTo(buf, nil, from)
	case from.IP.To16() != nil && t.v6 != nil:
		_, err = t.v6.WriteTo(buf, nil, from)
	default:
		return errors.New("discovery: no socket for the reply")
	}
	t.access.record(iface, err)
	return err
}

// close closes the sockets. Received messages are dropped.
func (t *transport) close() {
	t.closeOnce.Do(func() {
		close(t.done)
		if t.v4 != nil {
			_ = t.v4.Close()
		}
		if t.v6 != nil {
			_ = t.v6.Close()
		}
	})
}

func debugf(logger *slog.Logger, format string, args ...any) {
	if logger != nil {
		logger.Debug(fmt.Sprintf(format, args...))
	}
}
