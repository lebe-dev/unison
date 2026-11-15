package transport

import (
	"bytes"
	"context"
	"crypto/mldsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// TestAuthProofFormat checks the signed message and the frame layout.
func TestAuthProofFormat(t *testing.T) {
	if authLabel != "unison/1 auth" || authType != "auth" {
		t.Fatalf("signature context %q does not match the exporter label %q", authType, authLabel)
	}
	if authFrameSize != 1+1+32+mldsa.MLDSA65SignatureSize {
		t.Fatalf("frame size = %d", authFrameSize)
	}

	id := testIdentity(t, 1)
	device := id.DeviceID()
	ekm := bytes.Repeat([]byte{0xab}, authEKMSize)

	message := authMessage(ekm, model.RolePersonal, device)
	if len(message) != authEKMSize+1+authDeviceSize {
		t.Fatalf("message size = %d, want %d", len(message), authEKMSize+1+authDeviceSize)
	}
	if !bytes.Equal(message[:authEKMSize], ekm) {
		t.Fatal("message does not start with the exporter secret")
	}
	if message[authEKMSize] != byte(model.RolePersonal) {
		t.Fatalf("role byte = %d, want %d", message[authEKMSize], model.RolePersonal)
	}
	if !bytes.Equal(message[authEKMSize+1:], device[:]) {
		t.Fatal("message does not end with the device id")
	}

	signature, err := id.SignMLDSA(authType, message)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	frame := authFrame(model.RolePersonal, device, signature)
	if len(frame) != authFrameSize {
		t.Fatalf("frame = %d bytes, want %d", len(frame), authFrameSize)
	}
	if frame[0] != authVersion || frame[1] != byte(model.RolePersonal) {
		t.Fatalf("frame header = %v", frame[:2])
	}
	if !bytes.Equal(frame[2:2+authDeviceSize], device[:]) {
		t.Fatal("frame does not carry the device id")
	}
	if !bytes.Equal(frame[2+authDeviceSize:], signature) {
		t.Fatal("frame does not carry the signature")
	}
	if err := id.Public().VerifyMLDSA(authType, message, frame[2+authDeviceSize:]); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// A proof signed for another session must not verify: the exporter secret is
	// the channel binding.
	other := authMessage(bytes.Repeat([]byte{0xcd}, authEKMSize), model.RolePersonal, device)
	if err := id.Public().VerifyMLDSA(authType, other, frame[2+authDeviceSize:]); err == nil {
		t.Fatal("a proof signed for another exporter secret verified")
	}
}

// TestCheckConnectionState checks the post-handshake requirements, including a
// classical key exchange that reached the end of the handshake.
func TestCheckConnectionState(t *testing.T) {
	cert, err := x509.ParseCertificate(testIdentity(t, 1).Certificate().Certificate[0])
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	valid := quic.ConnectionState{TLS: tls.ConnectionState{
		HandshakeComplete:  true,
		Version:            tls.VersionTLS13,
		CipherSuite:        tls.TLS_AES_128_GCM_SHA256,
		CurveID:            tls.X25519MLKEM768,
		NegotiatedProtocol: ALPN,
		PeerCertificates:   []*x509.Certificate{cert},
	}}
	if err := checkConnectionState(valid, ALPN); err != nil {
		t.Fatalf("valid state: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*quic.ConnectionState)
		want   error
	}{
		{
			name:   "classical x25519",
			mutate: func(cs *quic.ConnectionState) { cs.TLS.CurveID = tls.X25519 },
			want:   ErrInsecureKeyExchange,
		},
		{
			name:   "classical p256",
			mutate: func(cs *quic.ConnectionState) { cs.TLS.CurveID = tls.CurveP256 },
			want:   ErrInsecureKeyExchange,
		},
		{
			name:   "zero curve",
			mutate: func(cs *quic.ConnectionState) { cs.TLS.CurveID = 0 },
			want:   ErrInsecureKeyExchange,
		},
		{
			name:   "tls 1.2",
			mutate: func(cs *quic.ConnectionState) { cs.TLS.Version = tls.VersionTLS12 },
			want:   ErrInsecureTransport,
		},
		{
			name:   "other protocol",
			mutate: func(cs *quic.ConnectionState) { cs.TLS.NegotiatedProtocol = "h3" },
			want:   ErrInsecureTransport,
		},
		{
			name:   "missing certificate",
			mutate: func(cs *quic.ConnectionState) { cs.TLS.PeerCertificates = nil },
			want:   ErrNoPeerCertificate,
		},
		{
			name:   "unfinished handshake",
			mutate: func(cs *quic.ConnectionState) { cs.TLS.HandshakeComplete = false },
			want:   ErrInsecureTransport,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := valid
			test.mutate(&state)
			if err := checkConnectionState(state, ALPN); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

// TestClassicalClientRefused checks that a client with a classical key exchange
// cannot connect to a listener.
func TestClassicalClientRefused(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	verifier := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	listener, err := NewListener(testPacketConn(t, network, "server:4433"), server, verifier, Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	classical := &tls.Config{
		Certificates:       []tls.Certificate{client.Certificate()},
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{ALPN},
		CurvePreferences:   []tls.CurveID{tls.X25519},
		InsecureSkipVerify: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := quic.Dial(ctx, testPacketConn(t, network, "client:4434"), listener.Addr(), classical, Config{}.withDefaults().quic())
	if err == nil {
		_ = conn.CloseWithError(0, "")
		t.Fatal("a classical client reached the listener")
	}
}

// TestClassicalServerRefused checks that a dialer does not connect to a server
// with a classical key exchange.
func TestClassicalServerRefused(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	classical := &tls.Config{
		Certificates:     []tls.Certificate{server.Certificate()},
		MinVersion:       tls.VersionTLS13,
		NextProtos:       []string{ALPN},
		CurvePreferences: []tls.CurveID{tls.X25519},
		ClientAuth:       tls.RequireAnyClientCert,
	}
	serverPC := testPacketConn(t, network, "server:4433")
	quicListener, err := quic.Listen(serverPC, classical, Config{}.withDefaults().quic())
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = quicListener.Close() })

	verifier := newTestVerifier().trust(peerOf(server, model.RolePersonal))
	dialer, err := NewDialer(testPacketConn(t, network, "client:4434"), client, verifier, Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if conn, err := dialer.Dial(ctx, quicListener.Addr()); err == nil {
		_ = conn.Close()
		t.Fatal("a classical server accepted the connection")
	}
}

// TestWrongAuthProof checks that a trusted device which sends a wrong
// authentication proof is disconnected.
func TestWrongAuthProof(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	other := testIdentity(t, 3)

	tests := []struct {
		name  string
		frame func(t *testing.T, ekm []byte) []byte
	}{
		{
			name: "role mismatch",
			frame: func(t *testing.T, ekm []byte) []byte {
				t.Helper()
				signature := signProof(t, client, ekm, model.RoleFriends, client.DeviceID())
				return authFrame(model.RoleFriends, client.DeviceID(), signature)
			},
		},
		{
			name: "device mismatch",
			frame: func(t *testing.T, ekm []byte) []byte {
				t.Helper()
				signature := signProof(t, client, ekm, model.RolePersonal, other.DeviceID())
				return authFrame(model.RolePersonal, other.DeviceID(), signature)
			},
		},
		{
			name: "another identity",
			frame: func(t *testing.T, ekm []byte) []byte {
				t.Helper()
				signature := signProof(t, other, ekm, model.RolePersonal, client.DeviceID())
				return authFrame(model.RolePersonal, client.DeviceID(), signature)
			},
		},
		{
			name: "another session",
			frame: func(t *testing.T, _ []byte) []byte {
				t.Helper()
				ekm := bytes.Repeat([]byte{0x01}, authEKMSize)
				signature := signProof(t, client, ekm, model.RolePersonal, client.DeviceID())
				return authFrame(model.RolePersonal, client.DeviceID(), signature)
			},
		},
		{
			name: "garbage signature",
			frame: func(*testing.T, []byte) []byte {
				return authFrame(model.RolePersonal, client.DeviceID(), bytes.Repeat([]byte{0x42}, mldsa.MLDSA65SignatureSize))
			},
		},
		{
			name: "unknown version",
			frame: func(t *testing.T, ekm []byte) []byte {
				t.Helper()
				signature := signProof(t, client, ekm, model.RolePersonal, client.DeviceID())
				frame := authFrame(model.RolePersonal, client.DeviceID(), signature)
				frame[0] = authVersion + 1
				return frame
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			network := vnet.NewNetwork()
			t.Cleanup(func() { _ = network.Close() })

			verifier := newTestVerifier().trust(peerOf(client, model.RolePersonal))
			listener, err := NewListener(testPacketConn(t, network, "server:4433"), server, verifier, Config{})
			if err != nil {
				t.Fatalf("listener: %v", err)
			}
			t.Cleanup(func() { _ = listener.Close() })

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			accepted := make(chan error, 1)
			go func() {
				conn, err := listener.Accept(ctx)
				if err == nil {
					_ = conn.Close()
				}
				accepted <- err
			}()

			qc := rawClient(t, testPacketConn(t, network, "client:4434"), listener.Addr(), client, trustKey(server))
			stream, err := qc.OpenStreamSync(ctx)
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}

			state := qc.ConnectionState()
			ekm, err := state.TLS.ExportKeyingMaterial(authLabel, nil, authEKMSize)
			if err != nil {
				t.Fatalf("export keying material: %v", err)
			}
			if _, err := stream.Write(test.frame(t, ekm)); err != nil {
				t.Fatalf("write proof: %v", err)
			}

			select {
			case err := <-accepted:
				if !errors.Is(err, ErrAuthProof) {
					t.Fatalf("accept error = %v, want %v", err, ErrAuthProof)
				}
			case <-ctx.Done():
				t.Fatal("accept did not reject the proof")
			}
		})
	}
}

// TestCorrectAuthProof checks that a raw client sending the expected proof is
// accepted, so the refusal tests fail on the proof itself.
func TestCorrectAuthProof(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	verifier := newTestVerifier().trust(peerOf(client, model.RolePersonal))
	listener, err := NewListener(testPacketConn(t, network, "server:4433"), server, verifier, Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	accepted := make(chan error, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err == nil {
			_ = conn.Close()
		}
		accepted <- err
	}()

	qc := rawClient(t, testPacketConn(t, network, "client:4434"), listener.Addr(), client, trustKey(server))
	stream, err := qc.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	state := qc.ConnectionState()
	ekm, err := state.TLS.ExportKeyingMaterial(authLabel, nil, authEKMSize)
	if err != nil {
		t.Fatalf("export keying material: %v", err)
	}
	signature := signProof(t, client, ekm, model.RolePersonal, client.DeviceID())
	if _, err := stream.Write(authFrame(model.RolePersonal, client.DeviceID(), signature)); err != nil {
		t.Fatalf("write proof: %v", err)
	}

	select {
	case err := <-accepted:
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("accept did not return")
	}
}

// signProof signs the authentication message of the identity.
func signProof(t *testing.T, id *identity.Identity, ekm []byte, role model.Role, device model.DeviceID) []byte {
	t.Helper()
	signature, err := id.SignMLDSA(authType, authMessage(ekm, role, device))
	if err != nil {
		t.Fatalf("sign proof: %v", err)
	}
	return signature
}

// fixedVerifier returns one peer for every certificate.
type fixedVerifier struct{ peer Peer }

// VerifyPeer returns the fixed peer.
func (v fixedVerifier) VerifyPeer(context.Context, *x509.Certificate) (Peer, error) {
	return v.peer, nil
}

// TestVerifierMismatch checks that a verifier which returns an identity that
// does not own the certificate is refused.
func TestVerifierMismatch(t *testing.T) {
	server := testIdentity(t, 1)
	client := testIdentity(t, 2)
	other := testIdentity(t, 3)
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	listener, err := NewListener(testPacketConn(t, network, "server:4433"), server, fixedVerifier{peerOf(other, model.RolePersonal)}, Config{})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	refused, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	accepted := make(chan error, 1)
	go func() {
		conn, err := listener.Accept(refused)
		if err == nil {
			_ = conn.Close()
		}
		accepted <- err
	}()

	dialerVerifier := newTestVerifier().trust(peerOf(server, model.RolePersonal))
	dialer, err := NewDialer(testPacketConn(t, network, "client:4434"), client, dialerVerifier, Config{})
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	if conn, err := dialer.Dial(ctx, listener.Addr()); err == nil {
		_ = conn.Close()
		t.Fatal("a verifier that returned another identity was accepted")
	}

	select {
	case err := <-accepted:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("accept error = %v, want a refused connection", err)
		}
	case <-ctx.Done():
		t.Fatal("accept did not return")
	}
}
