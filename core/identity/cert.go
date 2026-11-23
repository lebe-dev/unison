package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

// CertFingerprint is the SHA-256 hash of a DER encoded certificate.
type CertFingerprint [sha256.Size]byte

// String returns the lowercase hex form of the fingerprint.
func (f CertFingerprint) String() string {
	return hex.EncodeToString(f[:])
}

// Bytes returns a copy of the raw fingerprint.
func (f CertFingerprint) Bytes() []byte {
	return f[:]
}

// IsZero reports whether the fingerprint is all zeroes.
func (f CertFingerprint) IsZero() bool {
	return f == CertFingerprint{}
}

// ParseCertFingerprint parses a hex encoded fingerprint.
func ParseCertFingerprint(s string) (CertFingerprint, error) {
	var f CertFingerprint
	b, err := hex.DecodeString(s)
	if err != nil {
		return f, fmt.Errorf("identity: parse certificate fingerprint: %w", err)
	}
	if len(b) != len(f) {
		return f, fmt.Errorf("identity: parse certificate fingerprint: got %d bytes, want %d", len(b), len(f))
	}
	copy(f[:], b)
	return f, nil
}

// CertFingerprintOfDER returns the fingerprint of a DER encoded certificate.
func CertFingerprintOfDER(der []byte) CertFingerprint {
	return CertFingerprint(sha256.Sum256(der))
}

// CertFingerprintOf returns the fingerprint of a parsed certificate.
func CertFingerprintOf(cert *x509.Certificate) CertFingerprint {
	return CertFingerprintOfDER(cert.Raw)
}

// newCertificate creates the self-signed certificate for mutual TLS. The subject
// carries the device ID only, never a user visible device name.
func newCertificate(key ed25519.PrivateKey, id model.DeviceID, now time.Time) (tls.Certificate, error) {
	// Serial numbers are limited to 20 octets, the first 16 bytes of the device ID fit.
	serial := new(big.Int).SetBytes(id[:16])
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: id.String(),
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certificateValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		SignatureAlgorithm:    x509.PureEd25519,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("identity: create certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("identity: parse certificate: %w", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}
