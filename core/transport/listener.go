package transport

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
)

// Listener accepts authenticated connections from trusted peers.
type Listener struct {
	pc net.PacketConn

	transport *quic.Transport
	listener  *quic.Listener

	id  *identity.Identity
	ver PeerVerifier
	cfg Config
}

// NewListener binds a QUIC listener to pc. The listener takes ownership of pc
// and closes it with Close.
func NewListener(pc net.PacketConn, id *identity.Identity, ver PeerVerifier, cfg Config) (*Listener, error) {
	switch {
	case pc == nil:
		return nil, errors.New("transport: nil packet connection")
	case id == nil:
		return nil, errors.New("transport: nil identity")
	case ver == nil:
		return nil, errors.New("transport: nil peer verifier")
	}

	cfg = cfg.withDefaults()
	transport := &quic.Transport{Conn: pc}
	listener, err := transport.Listen(serverTLSConfig(id, ALPN, certificateVerifier(ver)), cfg.quic())
	if err != nil {
		return nil, fmt.Errorf("transport: listen: %w", err)
	}
	return &Listener{
		pc:        pc,
		transport: transport,
		listener:  listener,
		id:        id,
		ver:       ver,
		cfg:       cfg,
	}, nil
}

// Addr returns the local address of the listener.
func (l *Listener) Addr() net.Addr {
	return l.listener.Addr()
}

// Accept waits for the next authenticated connection. A peer that the verifier
// does not trust is refused during the TLS handshake and never appears here. A
// per-connection error, such as a failed authentication proof, does not stop
// the listener. Call Accept from several goroutines so that a peer which stalls
// the authentication proof does not hold up the others.
func (l *Listener) Accept(ctx context.Context) (*Conn, error) {
	qc, err := l.listener.Accept(ctx)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, l.cfg.HandshakeTimeout)
	defer cancel()

	conn, err := establish(ctx, qc, l.id, l.ver, false)
	if err != nil {
		closeOnError(qc, err)
		return nil, err
	}
	return conn, nil
}

// Close stops the listener, terminates its connections and closes the packet
// connection.
func (l *Listener) Close() error {
	err := l.listener.Close()
	if cerr := l.transport.Close(); err == nil {
		err = cerr
	}
	if cerr := l.pc.Close(); err == nil {
		err = cerr
	}
	return err
}

// Dialer opens authenticated connections to trusted peers.
type Dialer struct {
	pc net.PacketConn

	transport *quic.Transport

	id  *identity.Identity
	ver PeerVerifier
	cfg Config
}

// NewDialer binds a QUIC transport to pc. The dialer takes ownership of pc and
// closes it with Close.
func NewDialer(pc net.PacketConn, id *identity.Identity, ver PeerVerifier, cfg Config) (*Dialer, error) {
	switch {
	case pc == nil:
		return nil, errors.New("transport: nil packet connection")
	case id == nil:
		return nil, errors.New("transport: nil identity")
	case ver == nil:
		return nil, errors.New("transport: nil peer verifier")
	}

	cfg = cfg.withDefaults()
	return &Dialer{
		pc:        pc,
		transport: &quic.Transport{Conn: pc},
		id:        id,
		ver:       ver,
		cfg:       cfg,
	}, nil
}

// Dial opens an authenticated connection to addr. The returned connection is
// ready for the peer protocol.
func (d *Dialer) Dial(ctx context.Context, addr net.Addr) (*Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, d.cfg.HandshakeTimeout)
	defer cancel()

	tlsConfig := clientTLSConfig(d.id, ALPN, certificateVerifier(d.ver))
	qc, err := d.transport.Dial(ctx, addr, tlsConfig, d.cfg.quic())
	if err != nil {
		return nil, fmt.Errorf("transport: dial %s: %w", addr, err)
	}

	conn, err := establish(ctx, qc, d.id, d.ver, true)
	if err != nil {
		closeOnError(qc, err)
		return nil, err
	}
	return conn, nil
}

// DialPairing opens a pairing connection to addr. The peer is not paired yet,
// so the trust store is not consulted; a non-zero pin requires the certificate
// to match it, which is how the QR flow binds the identity. The pairing
// protocol runs on the streams of the connection.
func (d *Dialer) DialPairing(ctx context.Context, addr net.Addr, pin identity.CertFingerprint) (*PairingConn, error) {
	ctx, cancel := context.WithTimeout(ctx, d.cfg.HandshakeTimeout)
	defer cancel()

	tlsConfig := clientTLSConfig(d.id, PairingALPN, pairingCertificateVerifier(pin))
	qc, err := d.transport.Dial(ctx, addr, tlsConfig, d.cfg.quic())
	if err != nil {
		return nil, fmt.Errorf("transport: dial pairing %s: %w", addr, err)
	}

	conn, err := pairingConn(qc)
	if err != nil {
		closeOnError(qc, err)
		return nil, err
	}
	return conn, nil
}

// Close closes the packet connection and every connection of the dialer.
func (d *Dialer) Close() error {
	err := d.transport.Close()
	if cerr := d.pc.Close(); err == nil {
		err = cerr
	}
	return err
}

// PairingListener accepts connections from devices that are not paired yet. It
// runs on its own application protocol and leaves the trust decision to the
// pairing policy.
type PairingListener struct {
	pc net.PacketConn

	transport *quic.Transport
	listener  *quic.Listener
}

// NewPairingListener binds a pairing listener to pc. The listener takes
// ownership of pc and closes it with Close. A nil policy is refused: the caller
// decides between pinning a certificate and accepting any peer.
func NewPairingListener(pc net.PacketConn, id *identity.Identity, policy PairingPolicy, cfg Config) (*PairingListener, error) {
	switch {
	case pc == nil:
		return nil, errors.New("transport: nil packet connection")
	case id == nil:
		return nil, errors.New("transport: nil identity")
	case policy == nil:
		return nil, errors.New("transport: nil pairing policy")
	}

	cfg = cfg.withDefaults()
	transport := &quic.Transport{Conn: pc}
	listener, err := transport.Listen(serverTLSConfig(id, PairingALPN, pairingPolicyVerifier(policy)), cfg.quic())
	if err != nil {
		return nil, fmt.Errorf("transport: listen pairing: %w", err)
	}
	return &PairingListener{
		pc:        pc,
		transport: transport,
		listener:  listener,
	}, nil
}

// Addr returns the local address of the listener.
func (l *PairingListener) Addr() net.Addr {
	return l.listener.Addr()
}

// Accept waits for the next pairing connection. A peer that the policy refuses
// is refused during the TLS handshake and never appears here.
func (l *PairingListener) Accept(ctx context.Context) (*PairingConn, error) {
	qc, err := l.listener.Accept(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := pairingConn(qc)
	if err != nil {
		closeOnError(qc, err)
		return nil, err
	}
	return conn, nil
}

// Close stops the listener, terminates its connections and closes the packet
// connection.
func (l *PairingListener) Close() error {
	err := l.listener.Close()
	if cerr := l.transport.Close(); err == nil {
		err = cerr
	}
	if cerr := l.pc.Close(); err == nil {
		err = cerr
	}
	return err
}

// pairingConn checks a completed pairing handshake and builds the connection.
func pairingConn(qc *quic.Conn) (*PairingConn, error) {
	cs := qc.ConnectionState()
	if err := checkConnectionState(cs, PairingALPN); err != nil {
		return nil, err
	}
	peer, err := pairingPeerFrom(cs.TLS.PeerCertificates[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPairingRejected, err)
	}
	return &PairingConn{quic: qc, peer: peer}, nil
}

// PairingConn is a QUIC connection to a device that is not paired yet. The
// pairing protocol authenticates the devices on the streams.
type PairingConn struct {
	quic *quic.Conn
	peer PairingPeer
}

// Peer returns the pairing identity of the remote device.
func (c *PairingConn) Peer() PairingPeer {
	return c.peer
}

// OpenStream opens a bidirectional stream.
func (c *PairingConn) OpenStream(ctx context.Context) (*quic.Stream, error) {
	return c.quic.OpenStreamSync(ctx)
}

// AcceptStream waits for a bidirectional stream opened by the peer.
func (c *PairingConn) AcceptStream(ctx context.Context) (*quic.Stream, error) {
	return c.quic.AcceptStream(ctx)
}

// OpenUniStream opens a unidirectional stream.
func (c *PairingConn) OpenUniStream() (*quic.SendStream, error) {
	return c.quic.OpenUniStream()
}

// AcceptUniStream waits for a unidirectional stream opened by the peer.
func (c *PairingConn) AcceptUniStream(ctx context.Context) (*quic.ReceiveStream, error) {
	return c.quic.AcceptUniStream(ctx)
}

// CloseWithError closes the connection with an application error code.
func (c *PairingConn) CloseWithError(code quic.ApplicationErrorCode, message string) error {
	return c.quic.CloseWithError(code, message)
}

// Close closes the connection.
func (c *PairingConn) Close() error {
	return c.quic.CloseWithError(0, "")
}

// LocalAddr returns the local address of the connection.
func (c *PairingConn) LocalAddr() net.Addr {
	return c.quic.LocalAddr()
}

// RemoteAddr returns the peer address of the connection.
func (c *PairingConn) RemoteAddr() net.Addr {
	return c.quic.RemoteAddr()
}

// Context returns the context of the connection, which is cancelled when the
// connection ends.
func (c *PairingConn) Context() context.Context {
	return c.quic.Context()
}

// ConnectionState returns the QUIC and TLS state of the connection.
func (c *PairingConn) ConnectionState() quic.ConnectionState {
	return c.quic.ConnectionState()
}
