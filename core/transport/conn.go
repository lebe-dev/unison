package transport

import (
	"context"
	"crypto/mldsa"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// The authentication proof is the message both sides sign right after the
// handshake, and the term for the frame carrying it.
//
// Message, signed with ML-DSA-65:
//
//	exporter(32) || role(1) || device_id(32)
//
// exporter is ExportKeyingMaterial("unison/1 auth", nil, 32) of the TLS
// session. role is the model.Role the sender has for the peer as one byte, and
// device_id is the sender device ID. The signature type is "auth", so the
// ML-DSA context is "unison/1 auth", the same as the exporter label. The
// message binds the proof to this session, to the role and to the device.
//
// Frame on the stream, fixed size, no length prefix:
//
//	version(1) || role(1) || device_id(32) || signature(3309)
//
// Roles are symmetric by definition, so both sides send the role they have for
// the peer and both check it against the role their trust store returned.
const (
	authVersion    = 1
	authType       = "auth"
	authLabel      = "unison/1 auth"
	authEKMSize    = 32
	authDeviceSize = len(model.DeviceID{})
	authFrameSize  = 1 + 1 + authDeviceSize + mldsa.MLDSA65SignatureSize
)

// Conn is an authenticated QUIC connection to a trusted peer.
type Conn struct {
	quic    *quic.Conn
	peer    Peer
	control *quic.Stream
}

// Peer returns the authenticated peer.
func (c *Conn) Peer() Peer {
	return c.peer
}

// Role returns the authenticated trust role of the peer.
func (c *Conn) Role() model.Role {
	return c.peer.Role
}

// ControlStream returns the first bidirectional stream of the connection. The
// authentication proof was exchanged on it, the peer protocol continues there.
func (c *Conn) ControlStream() *quic.Stream {
	return c.control
}

// OpenStream opens a bidirectional stream.
func (c *Conn) OpenStream(ctx context.Context) (*quic.Stream, error) {
	return c.quic.OpenStreamSync(ctx)
}

// AcceptStream waits for a bidirectional stream opened by the peer.
func (c *Conn) AcceptStream(ctx context.Context) (*quic.Stream, error) {
	return c.quic.AcceptStream(ctx)
}

// OpenUniStream opens a unidirectional stream.
func (c *Conn) OpenUniStream() (*quic.SendStream, error) {
	return c.quic.OpenUniStream()
}

// AcceptUniStream waits for a unidirectional stream opened by the peer.
func (c *Conn) AcceptUniStream(ctx context.Context) (*quic.ReceiveStream, error) {
	return c.quic.AcceptUniStream(ctx)
}

// CloseWithError closes the connection with an application error code.
func (c *Conn) CloseWithError(code quic.ApplicationErrorCode, message string) error {
	return c.quic.CloseWithError(code, message)
}

// Close closes the connection.
func (c *Conn) Close() error {
	return c.quic.CloseWithError(0, "")
}

// LocalAddr returns the local address of the connection.
func (c *Conn) LocalAddr() net.Addr {
	return c.quic.LocalAddr()
}

// RemoteAddr returns the peer address of the connection.
func (c *Conn) RemoteAddr() net.Addr {
	return c.quic.RemoteAddr()
}

// Context returns the context of the connection, which is cancelled when the
// connection ends.
func (c *Conn) Context() context.Context {
	return c.quic.Context()
}

// ConnectionState returns the QUIC and TLS state of the connection.
func (c *Conn) ConnectionState() quic.ConnectionState {
	return c.quic.ConnectionState()
}

// establish checks a completed handshake and exchanges the authentication
// proof. The client opens the control stream, the server accepts it.
func establish(ctx context.Context, qc *quic.Conn, local *identity.Identity, v PeerVerifier, client bool) (*Conn, error) {
	cs := qc.ConnectionState()
	if err := checkConnectionState(cs, ALPN); err != nil {
		return nil, err
	}
	peer, err := verifiedPeer(ctx, v, cs.TLS.PeerCertificates[0])
	if err != nil {
		return nil, err
	}

	var stream *quic.Stream
	if client {
		stream, err = qc.OpenStreamSync(ctx)
	} else {
		stream, err = qc.AcceptStream(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("transport: open control stream: %w", err)
	}

	c := &Conn{quic: qc, peer: peer, control: stream}
	if err := c.authenticate(ctx, local); err != nil {
		return nil, err
	}
	return c, nil
}

// authenticate exchanges the authentication proof with the peer.
func (c *Conn) authenticate(ctx context.Context, local *identity.Identity) error {
	cs := c.quic.ConnectionState()
	ekm, err := cs.TLS.ExportKeyingMaterial(authLabel, nil, authEKMSize)
	if err != nil {
		return fmt.Errorf("transport: export keying material: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := c.control.SetDeadline(deadline); err != nil {
			return fmt.Errorf("transport: set proof deadline: %w", err)
		}
		defer func() { _ = c.control.SetDeadline(time.Time{}) }()
	}

	role := c.peer.Role
	device := local.DeviceID()
	signature, err := local.SignMLDSA(authType, authMessage(ekm, role, device))
	if err != nil {
		return fmt.Errorf("transport: sign authentication proof: %w", err)
	}
	if _, err := c.control.Write(authFrame(role, device, signature)); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthProof, err)
	}

	frame := make([]byte, authFrameSize)
	if _, err := io.ReadFull(c.control, frame); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthProof, err)
	}
	return c.checkAuthProof(frame, ekm)
}

// checkAuthProof verifies the frame the peer sent.
func (c *Conn) checkAuthProof(frame, ekm []byte) error {
	if len(frame) != authFrameSize {
		return fmt.Errorf("%w: proof of %d bytes", ErrAuthProof, len(frame))
	}
	if frame[0] != authVersion {
		return fmt.Errorf("%w: proof version %d", ErrAuthProof, frame[0])
	}
	role := model.Role(frame[1])
	device, err := model.DeviceIDFromBytes(frame[2 : 2+authDeviceSize])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuthProof, err)
	}
	signature := frame[2+authDeviceSize:]

	if role != c.peer.Role {
		return fmt.Errorf("%w: role %s, want %s", ErrAuthProof, role, c.peer.Role)
	}
	if device != c.peer.DeviceID() {
		return fmt.Errorf("%w: device mismatch", ErrAuthProof)
	}
	if err := c.peer.Public.VerifyMLDSA(authType, authMessage(ekm, role, device), signature); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthProof, err)
	}
	return nil
}

// authMessage returns the signed message of the authentication proof.
func authMessage(ekm []byte, role model.Role, device model.DeviceID) []byte {
	message := make([]byte, 0, len(ekm)+1+len(device))
	message = append(message, ekm...)
	message = append(message, byte(role))
	message = append(message, device[:]...)
	return message
}

// authFrame encodes the authentication proof for the stream.
func authFrame(role model.Role, device model.DeviceID, signature []byte) []byte {
	frame := make([]byte, 0, authFrameSize)
	frame = append(frame, authVersion, byte(role))
	frame = append(frame, device[:]...)
	return append(frame, signature...)
}

// closeOnError closes a connection that failed the transport checks.
func closeOnError(qc *quic.Conn, err error) {
	code := quic.ApplicationErrorCode(0)
	switch {
	case errors.Is(err, ErrInsecureKeyExchange), errors.Is(err, ErrInsecureTransport):
		code = CodeInsecure
	case errors.Is(err, ErrAuthProof):
		code = CodeAuthFailed
	}
	_ = qc.CloseWithError(code, "connection rejected")
}
