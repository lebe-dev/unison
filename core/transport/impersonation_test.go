package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// forgedCertificate returns a self-signed certificate that names the device of
// subject but carries the key of signer.
func forgedCertificate(t testing.TB, subject *identity.Identity, signer ed25519.PrivateKey) tls.Certificate {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: subject.DeviceID().String()},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, signer.Public(), signer)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: signer}
}

// attackerKey returns the Ed25519 key of an identity nobody trusts.
func attackerKey(t testing.TB) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// dialWith dials the listener of the pair with an arbitrary client
// certificate and reports whether the server finished the TLS handshake. A
// server that refuses the client closes the connection during the handshake;
// one that accepts it keeps the connection open and waits for the
// authentication proof, which this raw client never sends.
func dialWith(t *testing.T, pair *testPair, cert tls.Certificate) bool {
	t.Helper()

	pc := testPacketConn(t, pair.network, "attacker:5555")
	tr := &quic.Transport{Conn: pc}
	t.Cleanup(func() { _ = tr.Close() })

	// The listener has to run its accept loop for the handshake to happen.
	serving, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		if conn, err := pair.listener.Accept(serving); err == nil {
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{ALPN},
		CurvePreferences:   []tls.CurveID{tls.X25519MLKEM768},
		InsecureSkipVerify: true, //nolint:gosec // The test plays an attacker that accepts any server.
	}
	qc, err := tr.Dial(ctx, pair.listener.Addr(), config, &quic.Config{})
	if err != nil {
		return false
	}
	defer func() { _ = qc.CloseWithError(0, "") }()

	select {
	case <-qc.Context().Done():
		var transportErr *quic.TransportError
		if !errors.As(context.Cause(qc.Context()), &transportErr) || !transportErr.ErrorCode.IsCryptoError() {
			t.Fatalf("the server closed the connection with %v, want a TLS refusal", context.Cause(qc.Context()))
		}
		return false
	case <-time.After(time.Second):
		return true
	}
}

// TestForgedNameRefused checks that a certificate with the device ID of a
// trusted peer and another key is refused: trust follows the key, not the name.
func TestForgedNameRefused(t *testing.T) {
	server := testIdentity(t, 1)
	trusted := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(trusted, model.RolePersonal))
	pair := newTestPair(t, server, trusted, serverVer, newTestVerifier().trust(peerOf(server, model.RolePersonal)))

	if dialWith(t, pair, forgedCertificate(t, trusted, attackerKey(t))) {
		t.Fatal("a certificate with a trusted name and a foreign key was accepted")
	}
}

// TestStolenCertificateRefused checks that the certificate of a trusted peer
// is useless without its private key.
func TestStolenCertificateRefused(t *testing.T) {
	server := testIdentity(t, 1)
	trusted := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(trusted, model.RolePersonal))
	pair := newTestPair(t, server, trusted, serverVer, newTestVerifier().trust(peerOf(server, model.RolePersonal)))

	stolen := tls.Certificate{Certificate: trusted.Certificate().Certificate, PrivateKey: attackerKey(t)}
	if dialWith(t, pair, stolen) {
		t.Fatal("the certificate of a trusted peer without its key was accepted")
	}
}

// TestUnknownKeyRefused checks a certificate that is valid in form but
// belongs to nobody the device trusts.
func TestUnknownKeyRefused(t *testing.T) {
	server := testIdentity(t, 1)
	trusted := testIdentity(t, 2)
	stranger := testIdentity(t, 3)
	serverVer := newTestVerifier().trust(peerOf(trusted, model.RolePersonal))
	pair := newTestPair(t, server, trusted, serverVer, newTestVerifier().trust(peerOf(server, model.RolePersonal)))

	if dialWith(t, pair, stranger.Certificate()) {
		t.Fatal("a stranger was accepted")
	}
}

// TestTrustedCertificateAccepted is the control of the refusal tests: the same
// raw dial with the real certificate of the trusted peer gets through.
func TestTrustedCertificateAccepted(t *testing.T) {
	server := testIdentity(t, 1)
	trusted := testIdentity(t, 2)
	serverVer := newTestVerifier().trust(peerOf(trusted, model.RolePersonal))
	pair := newTestPair(t, server, trusted, serverVer, newTestVerifier().trust(peerOf(server, model.RolePersonal)))

	if !dialWith(t, pair, trusted.Certificate()) {
		t.Fatal("the trusted peer was refused")
	}
}

// FuzzPeerCertificate feeds arbitrary certificates to the check of the main
// listener. Only the exact key of a trusted peer may pass, whatever the
// certificate says about itself.
func FuzzPeerCertificate(f *testing.F) {
	trusted := testIdentity(f, 2)
	verifier := newTestVerifier().trust(peerOf(trusted, model.RolePersonal))
	check := certificateVerifier(verifier)

	f.Add(trusted.Certificate().Certificate[0])
	f.Add(testIdentity(f, 3).Certificate().Certificate[0])
	f.Add(forgedCertificate(f, trusted, attackerKey(f)).Certificate[0])
	f.Add([]byte{})
	f.Add([]byte{0x30, 0x82, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, der []byte) {
		err := check([][]byte{der}, nil)
		if err == nil {
			cert, parseErr := x509.ParseCertificate(der)
			if parseErr != nil {
				t.Fatalf("an unparsable certificate passed: %v", parseErr)
			}
			key, ok := cert.PublicKey.(ed25519.PublicKey)
			if !ok || !key.Equal(trusted.Public().Ed25519) {
				t.Fatal("a certificate with a foreign key passed")
			}
			if cert.Subject.CommonName != trusted.DeviceID().String() {
				t.Fatalf("a certificate named %q passed", cert.Subject.CommonName)
			}
			return
		}
		if !errors.Is(err, ErrPeerRejected) && !errors.Is(err, ErrNoPeerCertificate) {
			t.Fatalf("refusal is not reported as ErrPeerRejected: %v", err)
		}
	})
}
