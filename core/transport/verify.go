package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// certificateIdentity returns the device ID and the Ed25519 key of an identity
// certificate. The trust decision does not look at the certificate chain: every
// device signs its own certificate, so the key is the only identity it carries.
func certificateIdentity(cert *x509.Certificate) (model.DeviceID, ed25519.PublicKey, error) {
	if cert == nil {
		return model.DeviceID{}, nil, errors.New("no certificate")
	}
	if cert.PublicKeyAlgorithm != x509.Ed25519 {
		return model.DeviceID{}, nil, fmt.Errorf("public key algorithm %s", cert.PublicKeyAlgorithm)
	}
	key, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok || len(key) != ed25519.PublicKeySize {
		return model.DeviceID{}, nil, errors.New("unsupported ed25519 public key")
	}
	id, err := model.ParseDeviceID(cert.Subject.CommonName)
	if err != nil {
		return model.DeviceID{}, nil, fmt.Errorf("subject: %w", err)
	}
	return id, key, nil
}

// verifiedPeer checks the certificate and asks the verifier for the peer. Every
// rejection is reported as ErrPeerRejected without the details of the decision.
func verifiedPeer(ctx context.Context, v PeerVerifier, cert *x509.Certificate) (Peer, error) {
	id, key, err := certificateIdentity(cert)
	if err != nil {
		return Peer{}, fmt.Errorf("%w: %w", ErrPeerRejected, err)
	}
	peer, err := v.VerifyPeer(ctx, cert)
	if err != nil {
		return Peer{}, fmt.Errorf("%w: %w", ErrPeerRejected, err)
	}
	if err := peer.check(id, key); err != nil {
		return Peer{}, err
	}
	return peer, nil
}

// check verifies that the peer returned by the verifier owns the certificate.
func (p Peer) check(id model.DeviceID, key ed25519.PublicKey) error {
	if p.Public.DeviceID != id {
		return fmt.Errorf("%w: verifier returned another device", ErrPeerRejected)
	}
	if !bytes.Equal(p.Public.Ed25519, key) {
		return fmt.Errorf("%w: verifier returned another key", ErrPeerRejected)
	}
	public, err := identity.NewPublicIdentity(p.Public.Ed25519, p.Public.MLDSABytes())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPeerRejected, err)
	}
	if public.DeviceID != p.Public.DeviceID {
		return fmt.Errorf("%w: device id does not match the identity keys", ErrPeerRejected)
	}
	if p.Role != model.RolePersonal && p.Role != model.RoleFriends {
		return fmt.Errorf("%w: role %d", ErrPeerRejected, p.Role)
	}
	return nil
}

// certificateVerifier returns the TLS callback that refuses peers the verifier
// does not accept.
func certificateVerifier(v PeerVerifier) func(rawCerts [][]byte, chains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		cert, err := leafCertificate(rawCerts)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrPeerRejected, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), HandshakeTimeout)
		defer cancel()
		_, err = verifiedPeer(ctx, v, cert)
		return err
	}
}

// pairingPeerFrom returns the pairing identity of a certificate.
func pairingPeerFrom(cert *x509.Certificate) (PairingPeer, error) {
	id, _, err := certificateIdentity(cert)
	if err != nil {
		return PairingPeer{}, err
	}
	return PairingPeer{
		DeviceID:        id,
		CertFingerprint: identity.CertFingerprintOf(cert),
	}, nil
}

// pairingCertificateVerifier returns the client side callback of a pairing
// connection. A non-zero pin requires the certificate to match it.
func pairingCertificateVerifier(pin identity.CertFingerprint) func(rawCerts [][]byte, chains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		cert, err := leafCertificate(rawCerts)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrPairingRejected, err)
		}
		peer, err := pairingPeerFrom(cert)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrPairingRejected, err)
		}
		if pin.IsZero() {
			return nil
		}
		if peer.CertFingerprint != pin {
			return fmt.Errorf("%w: certificate fingerprint mismatch", ErrPairingRejected)
		}
		return nil
	}
}

// pairingPolicyVerifier returns the listener side callback of a pairing
// connection, which hands the certificate to the pairing policy.
func pairingPolicyVerifier(policy PairingPolicy) func(rawCerts [][]byte, chains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		cert, err := leafCertificate(rawCerts)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrPairingRejected, err)
		}
		peer, err := pairingPeerFrom(cert)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrPairingRejected, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), HandshakeTimeout)
		defer cancel()
		if err := policy(ctx, peer); err != nil {
			return fmt.Errorf("%w: %w", ErrPairingRejected, err)
		}
		return nil
	}
}

// leafCertificate returns the certificate a peer presented.
func leafCertificate(rawCerts [][]byte) (*x509.Certificate, error) {
	if len(rawCerts) == 0 {
		return nil, ErrNoPeerCertificate
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}

// checkConnectionState enforces the TLS requirements that the negotiated
// parameters alone cannot guarantee.
func checkConnectionState(cs quic.ConnectionState, protocol string) error {
	if !cs.TLS.HandshakeComplete {
		return fmt.Errorf("%w: handshake is not complete", ErrInsecureTransport)
	}
	if cs.TLS.Version != tls.VersionTLS13 {
		return fmt.Errorf("%w: TLS version %#04x", ErrInsecureTransport, cs.TLS.Version)
	}
	if cs.TLS.NegotiatedProtocol != protocol {
		return fmt.Errorf("%w: application protocol %q", ErrInsecureTransport, cs.TLS.NegotiatedProtocol)
	}
	if cs.TLS.CurveID != tls.X25519MLKEM768 {
		return fmt.Errorf("%w: key exchange %s", ErrInsecureKeyExchange, cs.TLS.CurveID)
	}
	if len(cs.TLS.PeerCertificates) == 0 {
		return ErrNoPeerCertificate
	}
	return nil
}

// serverTLSConfig returns the TLS configuration of a listener.
func serverTLSConfig(id *identity.Identity, protocol string, verify func([][]byte, [][]*x509.Certificate) error) *tls.Config {
	return &tls.Config{
		Certificates:           []tls.Certificate{id.Certificate()},
		MinVersion:             tls.VersionTLS13,
		NextProtos:             []string{protocol},
		CurvePreferences:       []tls.CurveID{tls.X25519MLKEM768},
		ClientAuth:             tls.RequireAnyClientCert,
		SessionTicketsDisabled: true,
		VerifyPeerCertificate:  verify,
	}
}

// clientTLSConfig returns the TLS configuration of a dialer. The chain is not
// verified against roots, because every device signs its own certificate; the
// peer callback makes the decision instead.
func clientTLSConfig(id *identity.Identity, protocol string, verify func([][]byte, [][]*x509.Certificate) error) *tls.Config {
	return &tls.Config{
		Certificates:           []tls.Certificate{id.Certificate()},
		MinVersion:             tls.VersionTLS13,
		NextProtos:             []string{protocol},
		CurvePreferences:       []tls.CurveID{tls.X25519MLKEM768},
		InsecureSkipVerify:     true, //nolint:gosec // Every device signs its own certificate, so VerifyPeerCertificate decides.
		SessionTicketsDisabled: true,
		VerifyPeerCertificate:  verify,
	}
}
