package relay

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// probe runs a probe with the test timeout.
func probe(t *testing.T, cfg Config) ProbeResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	result, err := Probe(ctx, cfg)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	return result
}

// expectFailure checks the outcome of one transport.
func expectFailure(t *testing.T, got TransportProbe, transport Transport, want Failure) {
	t.Helper()

	if got.Transport != transport {
		t.Fatalf("probe of %s reports transport %s", transport, got.Transport)
	}
	if got.Failure != want {
		t.Fatalf("probe of %s: failure %s, want %s (error: %v)", transport, got.Failure, want, got.Err)
	}
	if want == FailureNone && (!got.OK() || got.Err != nil) {
		t.Fatalf("probe of %s did not succeed: %v", transport, got.Err)
	}
	if want != FailureNone && (got.OK() || got.Err == nil) {
		t.Fatalf("probe of %s succeeded, want %s", transport, want)
	}
}

func TestProbeReachesBothTransports(t *testing.T) {
	server := startTestRelay(t)

	result := probe(t, server.client(t))

	for _, got := range []TransportProbe{result.QUIC, result.WebSocket} {
		expectFailure(t, got, got.Transport, FailureNone)
		if got.Latency <= 0 {
			t.Fatalf("probe of %s reports latency %v", got.Transport, got.Latency)
		}
		if got.Limits.MaxDatagramBytes != 1400 {
			t.Fatalf("probe of %s reports limits %+v", got.Transport, got.Limits)
		}
	}
	expectFailure(t, result.QUIC, TransportQUIC, FailureNone)
	expectFailure(t, result.WebSocket, TransportWebSocket, FailureNone)
}

func TestProbeReportsBadToken(t *testing.T) {
	server := startTestRelay(t)
	cfg := server.client(t)
	cfg.Token = "wrong"

	result := probe(t, cfg)

	expectFailure(t, result.QUIC, TransportQUIC, FailureUnauthorized)
	expectFailure(t, result.WebSocket, TransportWebSocket, FailureUnauthorized)
}

func TestProbeReportsBlockedUDP(t *testing.T) {
	server := startTestRelay(t)
	blackhole, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind blackhole: %v", err)
	}
	t.Cleanup(func() { _ = blackhole.Close() })

	cfg := server.client(t)
	cfg.QUICAddr = blackhole.LocalAddr().String()
	result := probe(t, cfg)

	expectFailure(t, result.QUIC, TransportQUIC, FailureTimeout)
	expectFailure(t, result.WebSocket, TransportWebSocket, FailureNone)
}

func TestProbeReportsNoServer(t *testing.T) {
	server := startTestRelay(t)
	cfg := server.client(t)
	cfg.URL = fmt.Sprintf("127.0.0.1:%d", freePort(t))

	result := probe(t, cfg)

	if result.QUIC.Failure != FailureTimeout && result.QUIC.Failure != FailureRefused {
		t.Fatalf("probe of QUIC: failure %s (error: %v)", result.QUIC.Failure, result.QUIC.Err)
	}
	expectFailure(t, result.WebSocket, TransportWebSocket, FailureRefused)
}

func TestProbeReportsUnknownHost(t *testing.T) {
	server := startTestRelay(t)
	cfg := server.client(t)
	cfg.URL = "relay.unison.invalid:443"

	result := probe(t, cfg)

	expectFailure(t, result.QUIC, TransportQUIC, FailureAddress)
	expectFailure(t, result.WebSocket, TransportWebSocket, FailureAddress)
}

func TestProbeReportsUntrustedCertificate(t *testing.T) {
	server := startTestRelay(t)
	cfg := server.client(t)
	cfg.TLS = nil

	result := probe(t, cfg)

	expectFailure(t, result.QUIC, TransportQUIC, FailureCertificate)
	expectFailure(t, result.WebSocket, TransportWebSocket, FailureCertificate)
}

func TestProbeReportsAServerThatIsNotARelay(t *testing.T) {
	web := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(web.Close)
	server := startTestRelay(t)
	cfg := server.client(t)
	cfg.URL = web.Listener.Addr().String()

	result := probe(t, cfg)

	expectFailure(t, result.WebSocket, TransportWebSocket, FailureNotRelay)
}

func TestProbeReportsAnUnavailableUpstream(t *testing.T) {
	web := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	t.Cleanup(web.Close)
	server := startTestRelay(t)
	cfg := server.client(t)
	cfg.URL = web.Listener.Addr().String()

	result := probe(t, cfg)

	expectFailure(t, result.WebSocket, TransportWebSocket, FailureUnavailable)
}

func TestProbeRejectsAnInvalidConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := Probe(ctx, Config{URL: "http://relay.example.com", Token: testToken}); err == nil {
		t.Fatal("probe accepted a relay address without TLS")
	}
	if _, err := Probe(ctx, Config{URL: "relay.example.com:443"}); err == nil {
		t.Fatal("probe accepted a configuration without a token")
	}
}
