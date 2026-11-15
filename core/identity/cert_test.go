package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"slices"
	"testing"
	"time"
)

func TestCertificate(t *testing.T) {
	id := newTestIdentity(t)
	cert := id.Certificate()

	if len(cert.Certificate) != 1 {
		t.Fatalf("certificate chain: got %d certificates, want 1", len(cert.Certificate))
	}
	if cert.Leaf == nil {
		t.Fatal("certificate: no parsed leaf")
	}
	if cert.PrivateKey == nil {
		t.Fatal("certificate: no private key")
	}

	leaf := cert.Leaf
	if got, want := leaf.Subject.CommonName, id.DeviceID().String(); got != want {
		t.Fatalf("common name: got %q, want %q", got, want)
	}
	if leaf.SignatureAlgorithm != x509.PureEd25519 {
		t.Fatalf("signature algorithm: got %v, want PureEd25519", leaf.SignatureAlgorithm)
	}
	if leaf.IsCA {
		t.Fatal("certificate is a CA")
	}
	if leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Fatalf("key usage: got %v, want DigitalSignature", leaf.KeyUsage)
	}
	for _, usage := range []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth} {
		if !slices.Contains(leaf.ExtKeyUsage, usage) {
			t.Fatalf("extended key usage: missing %v", usage)
		}
	}
	if got, want := leaf.PublicKey.(ed25519.PublicKey), id.Public().Ed25519; !got.Equal(want) {
		t.Fatal("certificate public key does not match the identity key")
	}
	if err := leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature); err != nil {
		t.Fatalf("self signature: %v", err)
	}
	if leaf.NotAfter.Sub(leaf.NotBefore) < 9*365*24*time.Hour {
		t.Fatalf("validity: got %s, want at least 9 years", leaf.NotAfter.Sub(leaf.NotBefore))
	}
	if !leaf.NotBefore.Before(time.Now()) || !leaf.NotAfter.After(time.Now()) {
		t.Fatalf("validity does not cover now: %s - %s", leaf.NotBefore, leaf.NotAfter)
	}
	if len(leaf.DNSNames) != 0 || len(leaf.IPAddresses) != 0 {
		t.Fatal("certificate carries host names")
	}
}

func TestCertFingerprint(t *testing.T) {
	id := newTestIdentity(t)

	want := sha256.Sum256(id.Certificate().Certificate[0])
	if got := id.CertFingerprint(); got != CertFingerprint(want) {
		t.Fatalf("fingerprint: got %s, want %x", got, want)
	}
	if got := CertFingerprintOf(id.Certificate().Leaf); got != id.CertFingerprint() {
		t.Fatalf("fingerprint of leaf: got %s, want %s", got, id.CertFingerprint())
	}

	parsed, err := ParseCertFingerprint(id.CertFingerprint().String())
	if err != nil {
		t.Fatalf("parse fingerprint: %v", err)
	}
	if parsed != id.CertFingerprint() {
		t.Fatalf("parsed fingerprint: got %s, want %s", parsed, id.CertFingerprint())
	}
	if got, want := len(id.CertFingerprint().Bytes()), sha256.Size; got != want {
		t.Fatalf("fingerprint length: got %d, want %d", got, want)
	}
	var zero CertFingerprint
	if !zero.IsZero() || id.CertFingerprint().IsZero() {
		t.Fatal("IsZero reports the wrong value")
	}

	if _, err := ParseCertFingerprint("not hex"); err == nil {
		t.Fatal("parse invalid fingerprint: got nil error, want an error")
	}
	if _, err := ParseCertFingerprint("00"); err == nil {
		t.Fatal("parse short fingerprint: got nil error, want an error")
	}
}

func TestCertificateIsStable(t *testing.T) {
	id := newTestIdentity(t)
	now := time.Now()

	first, err := newCertificate(id.edKey, id.DeviceID(), now)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	second, err := newCertificate(id.edKey, id.DeviceID(), now)
	if err != nil {
		t.Fatalf("create certificate again: %v", err)
	}
	if !slices.Equal(first.Certificate[0], second.Certificate[0]) {
		t.Fatal("certificate is not deterministic for the same key, device id and time")
	}
}

func TestCertificateHandshake(t *testing.T) {
	id := newTestIdentity(t)
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	server := tls.Server(serverConn, &tls.Config{
		Certificates: []tls.Certificate{id.Certificate()},
		MinVersion:   tls.VersionTLS13,
	})
	client := tls.Client(clientConn, &tls.Config{
		// The peer is pinned by the certificate fingerprint instead of a CA chain.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) != 1 {
				return errors.New("unexpected certificate chain")
			}
			if CertFingerprintOfDER(rawCerts[0]) != id.CertFingerprint() {
				return errors.New("unexpected certificate fingerprint")
			}
			return nil
		},
	})

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Handshake()
	}()
	if err := client.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
	if state := client.ConnectionState(); state.Version != tls.VersionTLS13 {
		t.Fatalf("protocol version: got %d, want TLS 1.3", state.Version)
	}
}
