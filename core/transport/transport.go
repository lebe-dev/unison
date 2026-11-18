package transport

import (
	"context"
	"crypto/x509"
	"errors"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// Application protocols and connection defaults.
const (
	// ALPN is the application protocol of peer connections.
	ALPN = "unison/1"
	// PairingALPN is the application protocol of pairing connections.
	PairingALPN = "unison-pair/1"

	// KeepAlivePeriod is the interval of keepalive packets on an idle connection.
	// Every keepalive wakes an idle phone, so it is as long as the idle timeout
	// allows with room for one lost packet.
	KeepAlivePeriod = 25 * time.Second
	// IdleTimeout closes a connection when the peer stays silent for this long.
	IdleTimeout = 60 * time.Second
	// HandshakeTimeout bounds the handshake, the peer verification and the
	// authentication proof exchange.
	HandshakeTimeout = 10 * time.Second

	// MaxIncomingStreams bounds the concurrently open bidirectional streams of a peer.
	MaxIncomingStreams = 32
	// MaxIncomingUniStreams bounds the concurrently open unidirectional streams of a peer.
	MaxIncomingUniStreams = 128

	// MinPacketSize is the smallest QUIC packet a handshake can use.
	MinPacketSize = 1200
	// MaxPacketSize is the largest QUIC packet a connection can carry.
	MaxPacketSize = 1452
)

// Application error codes that the transport sends when it closes a connection.
const (
	// CodeAuthFailed closes a connection whose authentication proof did not verify.
	CodeAuthFailed quic.ApplicationErrorCode = 0x01
	// CodeInsecure closes a connection that does not meet the transport
	// requirements, for example one without the post-quantum key exchange.
	CodeInsecure quic.ApplicationErrorCode = 0x02
)

// Errors reported by the transport.
var (
	// ErrPeerRejected reports a peer whose certificate is not accepted.
	ErrPeerRejected = errors.New("transport: peer rejected")
	// ErrPairingRejected reports a pairing peer that the pairing policy refused.
	ErrPairingRejected = errors.New("transport: pairing peer rejected")
	// ErrNoPeerCertificate reports a peer that presented no certificate.
	ErrNoPeerCertificate = errors.New("transport: peer certificate missing")
	// ErrInsecureTransport reports a connection without TLS 1.3 and the
	// expected application protocol.
	ErrInsecureTransport = errors.New("transport: insecure connection")
	// ErrInsecureKeyExchange reports a connection without the hybrid
	// post-quantum key exchange.
	ErrInsecureKeyExchange = errors.New("transport: post-quantum key exchange required")
	// ErrAuthProof reports a peer whose authentication proof did not verify.
	ErrAuthProof = errors.New("transport: authentication proof rejected")
)

// Config tunes connection setup. A zero field keeps the package default.
type Config struct {
	// KeepAlive is the interval of keepalive packets on an idle connection.
	KeepAlive time.Duration
	// IdleTimeout closes a connection when the peer stays silent for this long.
	IdleTimeout time.Duration
	// HandshakeTimeout bounds the handshake, the peer verification and the
	// authentication proof exchange.
	HandshakeTimeout time.Duration
	// MaxIncomingStreams bounds the concurrently open bidirectional streams.
	MaxIncomingStreams int64
	// MaxIncomingUniStreams bounds the concurrently open unidirectional streams.
	MaxIncomingUniStreams int64
	// PacketSize is the largest QUIC packet the connection sends, in bytes. It
	// is raised to MinPacketSize and turns path MTU discovery off. A zero value
	// keeps the default size and lets discovery grow it.
	PacketSize int
}

// withDefaults fills the zero fields with the package defaults.
func (c Config) withDefaults() Config {
	if c.KeepAlive <= 0 {
		c.KeepAlive = KeepAlivePeriod
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = IdleTimeout
	}
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = HandshakeTimeout
	}
	if c.MaxIncomingStreams <= 0 {
		c.MaxIncomingStreams = MaxIncomingStreams
	}
	if c.MaxIncomingUniStreams <= 0 {
		c.MaxIncomingUniStreams = MaxIncomingUniStreams
	}
	return c
}

// quic returns the QUIC configuration described by c.
func (c Config) quic() *quic.Config {
	cfg := &quic.Config{
		KeepAlivePeriod:       c.KeepAlive,
		MaxIdleTimeout:        c.IdleTimeout,
		HandshakeIdleTimeout:  c.HandshakeTimeout,
		MaxIncomingStreams:    c.MaxIncomingStreams,
		MaxIncomingUniStreams: c.MaxIncomingUniStreams,
	}
	if c.PacketSize > 0 {
		// A fixed packet size keeps the packets of a connection inside the
		// datagrams that carry them, so path MTU discovery stays off.
		cfg.InitialPacketSize = uint16(min(max(c.PacketSize, MinPacketSize), MaxPacketSize))
		cfg.DisablePathMTUDiscovery = true
	}
	return cfg
}

// Peer is the authenticated remote device of a connection.
type Peer struct {
	// Public is the public identity of the device, including its ML-DSA key.
	Public identity.PublicIdentity
	// Role is the trust role the local device has for the peer.
	Role model.Role
}

// DeviceID returns the device ID of the peer.
func (p Peer) DeviceID() model.DeviceID {
	return p.Public.DeviceID
}

// PeerVerifier decides whether a peer certificate belongs to a trusted device.
// The trust store implements it.
//
// VerifyPeer is called during the TLS handshake to refuse an untrusted peer and
// once more after the handshake to collect the identity and the role. Both
// calls must accept the peer, otherwise the connection is closed. An
// implementation fails closed: unknown, revoked or malformed devices return an
// error.
type PeerVerifier interface {
	VerifyPeer(ctx context.Context, cert *x509.Certificate) (Peer, error)
}

// PairingPeer identifies a device that is being paired by its certificate.
type PairingPeer struct {
	// DeviceID is the device ID carried by the certificate.
	DeviceID model.DeviceID
	// CertFingerprint is the SHA-256 fingerprint of the certificate. It is the
	// value a pairing QR code pins.
	CertFingerprint identity.CertFingerprint
}

// PairingPolicy decides whether a pairing connection is accepted. It runs
// during the TLS handshake, and an error refuses the connection. A pairing
// listener accepts unknown certificates and leaves the decision here.
type PairingPolicy func(ctx context.Context, peer PairingPeer) error

// AcceptPairingPeer accepts every pairing peer. The SAS flow compares the
// devices out of band, so the connection itself carries no pin.
func AcceptPairingPeer(context.Context, PairingPeer) error {
	return nil
}

// PinCertificate returns a pairing policy that accepts only the certificate
// with the given fingerprint. The QR flow pins the fingerprint it carries.
func PinCertificate(fp identity.CertFingerprint) PairingPolicy {
	return func(_ context.Context, peer PairingPeer) error {
		if peer.CertFingerprint != fp {
			return errors.New("certificate fingerprint mismatch")
		}
		return nil
	}
}
