package relay

import (
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/lebe-dev/unison/internal/observability"
)

// listenHTTP binds the health and metrics listener. It serves plain HTTP, so an
// operator can scrape it from the local network or a sidecar.
func (s *Server) listenHTTP() error {
	ln, err := net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("relay: listen health: %w", err)
	}
	s.httpListener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: httpHeaderTimeout,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer observability.Recover()
		err := s.httpServer.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("health listener stopped", "error", err)
			observability.CaptureMessage("relay: health listener stopped")
		}
	}()
	return nil
}

// handleMetrics renders the counters in the Prometheus text format.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.write(w)
}

// handleHealth reports that the process runs.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// handleReady reports that the server accepts connections.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.ctx.Err() != nil {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
