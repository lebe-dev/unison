package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/lebe-dev/unison/internal/observability"
	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// wsWriteTimeout bounds one write to a WebSocket client.
const wsWriteTimeout = 10 * time.Second

// listenWS binds the TLS listener of the WebSocket fallback.
func (s *Server) listenWS(cert tls.Certificate) error {
	ln, err := net.Listen("tcp", s.cfg.WSAddr)
	if err != nil {
		return fmt.Errorf("relay: listen WebSocket: %w", err)
	}
	s.wsListener = ln

	mux := http.NewServeMux()
	mux.HandleFunc(WSPath, s.handleWS)
	s.wsServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: httpHeaderTimeout,
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer observability.Recover()
		err := s.wsServer.Serve(tls.NewListener(ln, tlsConfig))
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("websocket listener stopped", "error", err)
			observability.CaptureMessage("relay: websocket listener stopped")
		}
	}()
	return nil
}

// handleWS upgrades an HTTP request to a WebSocket connection.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.capacity() {
		s.metrics.registration(relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE)
		s.log.Debug("connection refused, the relay is at capacity",
			"transport", transportLabels[transportWS],
		)
		http.Error(w, statusLabels[relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE], http.StatusServiceUnavailable)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{WSSubprotocol},
	})
	if err != nil {
		s.log.Debug("websocket upgrade failed", "error", err)
		return
	}
	if conn.Subprotocol() != WSSubprotocol {
		s.log.Debug("websocket subprotocol rejected", "subprotocol", conn.Subprotocol())
		_ = conn.Close(websocket.StatusPolicyViolation, "subprotocol required")
		return
	}
	conn.SetReadLimit(int64(max(s.cfg.MaxDatagramBytes+frameOverhead, maxRegisterBytes)))

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.serveWS(conn)
	}()
}

// serveWS registers a connection and relays its frames. One WebSocket message
// carries one frame.
func (s *Server) serveWS(conn *websocket.Conn) {
	p := s.newPeer(transportWS, &wsWriter{conn: conn})
	defer p.stop(relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED)
	defer observability.Recover()

	ctx, cancel := context.WithTimeout(s.ctx, s.cfg.RegisterTimeout)
	defer cancel()

	typ, data, err := conn.Read(ctx)
	if err != nil {
		s.log.Debug("registration not received",
			"conn", p.id,
			"transport", transportLabels[p.transport],
			"error", err,
		)
		return
	}
	if typ != websocket.MessageBinary {
		p.stop(relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT)
		return
	}
	frame, err := parseFrame(data, maxRegisterBytes)
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
		if err := conn.Write(ctx, websocket.MessageBinary, result); err != nil {
			s.log.Debug("registration answer not written",
				"conn", p.id,
				"transport", transportLabels[p.transport],
				"error", err,
			)
			return
		}
	}
	if status != relayv1.RelayStatus_RELAY_STATUS_OK {
		p.stop(status)
		return
	}

	go p.writeLoop()
	go s.pingPeer(p, conn)

	for {
		typ, data, err := conn.Read(context.Background())
		if err != nil {
			s.log.Debug("websocket closed",
				"conn", p.id,
				"transport", transportLabels[p.transport],
				"error", err,
			)
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		s.forward(p, data)
	}
}

// pingPeer keeps a WebSocket connection alive and detects a dead peer.
func (s *Server) pingPeer(p *peer, conn *websocket.Conn) {
	defer observability.Recover()

	ticker := time.NewTicker(s.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), s.cfg.PingInterval)
			err := conn.Ping(ctx)
			cancel()
			if err != nil {
				s.log.Debug("websocket ping failed",
					"conn", p.id,
					"transport", transportLabels[p.transport],
					"error", err,
				)
				p.stop(relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED)
				return
			}
		}
	}
}

// wsWriter sends frames as WebSocket binary messages.
type wsWriter struct {
	conn *websocket.Conn
}

// write sends one message.
func (w *wsWriter) write(frame []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
	defer cancel()
	return w.conn.Write(ctx, websocket.MessageBinary, frame)
}

// close ends the connection with the close code of the status.
func (w *wsWriter) close(status relayv1.RelayStatus) error {
	code := websocket.StatusNormalClosure
	reason := ""
	if status != relayv1.RelayStatus_RELAY_STATUS_UNSPECIFIED && status != relayv1.RelayStatus_RELAY_STATUS_OK {
		code = wsCloseCode(status)
		reason = statusLabels[int(status)]
	}
	return w.conn.Close(code, reason)
}

// wsCloseCode maps a status to a close code of the application range.
func wsCloseCode(status relayv1.RelayStatus) websocket.StatusCode {
	return websocket.StatusCode(4000 + int(status))
}
