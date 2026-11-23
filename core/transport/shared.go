package transport

import (
	"errors"
	"fmt"
	"net"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
)

// Shared is a QUIC transport bound to a packet connection. A listener and a
// dialer share it when both directions of a connection run on one packet
// connection, which the relay needs. The caller closes the Shared, which closes
// the packet connection and every connection on it.
type Shared struct {
	pc   net.PacketConn
	quic *quic.Transport
}

// NewShared binds a QUIC transport to pc and takes ownership of it.
func NewShared(pc net.PacketConn) (*Shared, error) {
	if pc == nil {
		return nil, errors.New("transport: nil packet connection")
	}
	return &Shared{pc: pc, quic: &quic.Transport{Conn: pc}}, nil
}

// Listener starts a listener on the transport. Closing the listener leaves the
// transport running.
func (s *Shared) Listener(id *identity.Identity, ver PeerVerifier, cfg Config) (*Listener, error) {
	switch {
	case id == nil:
		return nil, errors.New("transport: nil identity")
	case ver == nil:
		return nil, errors.New("transport: nil peer verifier")
	}

	cfg = cfg.withDefaults()
	listener, err := s.quic.Listen(serverTLSConfig(id, ALPN, certificateVerifier(ver)), cfg.quic())
	if err != nil {
		return nil, fmt.Errorf("transport: listen: %w", err)
	}
	return &Listener{listener: listener, id: id, ver: ver, cfg: cfg}, nil
}

// Dialer returns a dialer on the transport. Closing the dialer leaves the
// transport running.
func (s *Shared) Dialer(id *identity.Identity, ver PeerVerifier, cfg Config) (*Dialer, error) {
	switch {
	case id == nil:
		return nil, errors.New("transport: nil identity")
	case ver == nil:
		return nil, errors.New("transport: nil peer verifier")
	}

	return &Dialer{id: id, ver: ver, cfg: cfg.withDefaults(), transport: s.quic, shared: true}, nil
}

// Close closes the transport, its connections and the packet connection.
func (s *Shared) Close() error {
	err := s.quic.Close()
	if cerr := s.pc.Close(); err == nil && !errors.Is(cerr, net.ErrClosed) {
		err = cerr
	}
	return err
}
