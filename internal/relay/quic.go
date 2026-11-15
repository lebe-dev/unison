package relay

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/quic-go/quic-go"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// rejectDrainTimeout bounds the wait for a client to close a refused stream.
const rejectDrainTimeout = 2 * time.Second

// Application error codes that the QUIC transport closes a connection with.
const (
	codeUnauthorized      quic.ApplicationErrorCode = 0x01
	codeQuotaExceeded     quic.ApplicationErrorCode = 0x02
	codeInvalidArgument   quic.ApplicationErrorCode = 0x03
	codeUnavailable       quic.ApplicationErrorCode = 0x04
	codeDatagramsRequired quic.ApplicationErrorCode = 0x05
	codeShuttingDown      quic.ApplicationErrorCode = 0x06
)

// appCode maps a close status to the application error code.
func appCode(status relayv1.RelayStatus) quic.ApplicationErrorCode {
	switch status {
	case relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED:
		return codeUnauthorized
	case relayv1.RelayStatus_RELAY_STATUS_QUOTA_EXCEEDED:
		return codeQuotaExceeded
	case relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT:
		return codeInvalidArgument
	case relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE:
		return codeUnavailable
	case relayv1.RelayStatus_RELAY_STATUS_DATAGRAMS_REQUIRED:
		return codeDatagramsRequired
	case relayv1.RelayStatus_RELAY_STATUS_SHUTTING_DOWN:
		return codeShuttingDown
	case relayv1.RelayStatus_RELAY_STATUS_OK, relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED:
		return 0
	default:
		return codeInvalidArgument
	}
}

// quicTLSConfig returns the TLS configuration of the QUIC listener. Tokens
// authenticate the clients, so they present no certificate.
func quicTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{ALPN},
	}
}

// quicConfig returns the QUIC configuration of the server. Datagrams carry the
// traffic and the only stream is the registration.
func (s *Server) quicConfig() *quic.Config {
	return &quic.Config{
		EnableDatagrams:       true,
		MaxIdleTimeout:        quicIdleTimeout,
		KeepAlivePeriod:       quicKeepAlive,
		HandshakeIdleTimeout:  quicHandshakeTimeout,
		MaxIncomingStreams:    4,
		MaxIncomingUniStreams: -1,
	}
}

// listenQUIC binds the UDP socket and starts the QUIC listener.
func (s *Server) listenQUIC(cert tls.Certificate) error {
	addr, err := net.ResolveUDPAddr("udp", s.cfg.QUICAddr)
	if err != nil {
		return fmt.Errorf("relay: resolve QUIC address: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("relay: listen QUIC: %w", err)
	}
	s.udpConn = conn
	s.quicTransport = &quic.Transport{Conn: conn}
	listener, err := s.quicTransport.Listen(quicTLSConfig(cert), s.quicConfig())
	if err != nil {
		return fmt.Errorf("relay: listen QUIC: %w", err)
	}
	s.quicListener = listener

	s.wg.Add(1)
	go s.serveQUIC()
	return nil
}

// serveQUIC accepts connections until the server closes.
func (s *Server) serveQUIC() {
	defer s.wg.Done()

	for {
		conn, err := s.quicListener.Accept(s.ctx)
		if err != nil {
			return
		}
		if !s.capacity() {
			s.metrics.registration(relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE)
			_ = conn.CloseWithError(codeUnavailable, statusLabels[relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE])
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serveQUICConn(conn)
		}()
	}
}

// serveQUICConn registers a connection and relays its datagrams.
func (s *Server) serveQUICConn(conn *quic.Conn) {
	p := s.newPeer(transportQUIC, &quicWriter{conn: conn})
	defer p.stop(relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED)

	state := conn.ConnectionState()
	if !state.SupportsDatagrams.Local || !state.SupportsDatagrams.Remote {
		s.metrics.registration(relayv1.RelayStatus_RELAY_STATUS_DATAGRAMS_REQUIRED)
		p.stop(relayv1.RelayStatus_RELAY_STATUS_DATAGRAMS_REQUIRED)
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, s.cfg.RegisterTimeout)
	defer cancel()

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return
	}
	if err := stream.SetDeadline(time.Now().Add(s.cfg.RegisterTimeout)); err != nil {
		return
	}
	frame, err := readStreamFrame(bufio.NewReaderSize(stream, streamBufferSize), maxRegisterBytes)
	if err != nil {
		s.metrics.registration(relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)
		p.stop(relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)
		return
	}

	status := relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT
	if reg := frame.GetRegister(); reg != nil {
		status = s.register(p, reg)
	} else {
		s.metrics.registration(status)
	}
	if result := registerResultFrame(status, s.limits()); result != nil {
		_ = writeStreamFrame(stream, result)
	}
	_ = stream.Close()
	if status != relayv1.RelayStatus_RELAY_STATUS_OK {
		// Give the client a moment to read the answer before the connection
		// ends, since a close discards unsent stream data.
		awaitPeerClose(stream, rejectDrainTimeout)
		p.stop(status)
		return
	}

	go p.writeLoop()
	for {
		data, err := conn.ReceiveDatagram(conn.Context())
		if err != nil {
			return
		}
		s.forward(p, data)
	}
}

// awaitPeerClose waits until the peer closes its side of the stream. It reads
// no more than a small buffer, so a peer cannot keep the caller busy.
func awaitPeerClose(stream *quic.Stream, timeout time.Duration) {
	if err := stream.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return
	}
	buf := make([]byte, 128)
	total := 0
	for total < 1024 {
		n, err := stream.Read(buf)
		total += n
		if err != nil {
			return
		}
	}
}

// quicWriter sends frames as QUIC datagrams.
type quicWriter struct {
	conn *quic.Conn
}

// write sends one datagram. A datagram above the packet capacity is reported
// as too large, which the caller counts and drops.
func (w *quicWriter) write(frame []byte) error {
	if err := w.conn.SendDatagram(frame); err != nil {
		var tooLarge *quic.DatagramTooLargeError
		if errors.As(err, &tooLarge) {
			return fmt.Errorf("%w: %w", errFrameTooLarge, err)
		}
		return err
	}
	return nil
}

// close ends the connection with the application code of the status.
func (w *quicWriter) close(status relayv1.RelayStatus) error {
	reason := ""
	if status != relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED && status != relayv1.RelayStatus_RELAY_STATUS_OK {
		reason = statusLabels[int(status)]
	}
	return w.conn.CloseWithError(appCode(status), reason)
}
