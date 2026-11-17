package relay

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// noApplicationProtocol is the QUIC code of the TLS alert a server sends when
// it does not speak the requested application protocol.
const noApplicationProtocol = quic.TransportErrorCode(0x100 + 120)

// Failure is the reason a probe of one transport failed.
type Failure uint8

// Probe failures, from the network up to the relay protocol.
const (
	// FailureNone reports a probe that registered on the relay.
	FailureNone Failure = iota
	// FailureAddress reports a host name that does not resolve.
	FailureAddress
	// FailureTimeout reports a relay that did not answer in time: the host is
	// down, a firewall drops the packets or the network blocks UDP.
	FailureTimeout
	// FailureRefused reports a host that refused the connection: nothing
	// listens on the port.
	FailureRefused
	// FailureCertificate reports a TLS certificate the device does not trust or
	// that does not match the host.
	FailureCertificate
	// FailureNotRelay reports a server that answers but is not a relay, or a
	// proxy that does not forward the relay path.
	FailureNotRelay
	// FailureUnavailable reports a relay that is full, shutting down, or a proxy
	// whose relay does not answer.
	FailureUnavailable
	// FailureUnauthorized reports an access token the relay does not know.
	FailureUnauthorized
	// FailureQuota reports a token that used up its traffic quota.
	FailureQuota
	// FailureDatagramsTooSmall reports a relay whose datagrams cannot carry the
	// packets of a device.
	FailureDatagramsTooSmall
	// FailureOther reports any other error.
	FailureOther
)

// String returns the name of the failure for logs.
func (f Failure) String() string {
	switch f {
	case FailureNone:
		return "none"
	case FailureAddress:
		return "address"
	case FailureTimeout:
		return "timeout"
	case FailureRefused:
		return "refused"
	case FailureCertificate:
		return "certificate"
	case FailureNotRelay:
		return "not a relay"
	case FailureUnavailable:
		return "unavailable"
	case FailureUnauthorized:
		return "unauthorized"
	case FailureQuota:
		return "quota exceeded"
	case FailureDatagramsTooSmall:
		return "datagrams too small"
	default:
		return "other"
	}
}

// TransportProbe is the outcome of a probe of one transport.
type TransportProbe struct {
	// Transport is the transport of the probe.
	Transport Transport
	// Failure is the reason of a failed probe, FailureNone on success.
	Failure Failure
	// Err is the error of a failed probe.
	Err error
	// Latency is the time to connect and register.
	Latency time.Duration
	// Limits are the limits the relay announced on success.
	Limits Limits
}

// OK reports a probe that registered on the relay.
func (p TransportProbe) OK() bool {
	return p.Failure == FailureNone
}

// ProbeResult is the outcome of a probe of both transports.
type ProbeResult struct {
	QUIC      TransportProbe
	WebSocket TransportProbe
}

// Probe checks that the relay of the configuration accepts the device: it
// connects over each transport, registers one throwaway mailbox with the token
// and disconnects. The transports are probed at the same time. The error
// reports a configuration that cannot be probed at all.
func Probe(ctx context.Context, cfg Config) (ProbeResult, error) {
	d, err := NewDialer(cfg)
	if err != nil {
		return ProbeResult{}, err
	}
	var id ID
	_, _ = rand.Read(id[:])

	var result ProbeResult
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		result.QUIC = d.probe(ctx, TransportQUIC, d.dialQUIC, id)
	}()
	go func() {
		defer wg.Done()
		result.WebSocket = d.probe(ctx, TransportWebSocket, d.dialWebSocket, id)
	}()
	wg.Wait()
	return result, nil
}

// probe connects over one transport and registers the mailbox.
func (d *Dialer) probe(ctx context.Context, transport Transport, dial func(context.Context) (carrier, error), id ID) TransportProbe {
	out := TransportProbe{Transport: transport}
	start := time.Now()

	dialCtx, cancel := context.WithTimeout(ctx, d.cfg.DialTimeout)
	conn, err := dial(dialCtx)
	cancel()
	if err != nil {
		return out.failed(err)
	}
	defer func() { _ = conn.close() }()

	limits, err := conn.register(ctx, d.cfg.Token, []ID{id})
	if err != nil {
		return out.failed(err)
	}
	out.Latency = time.Since(start)
	out.Limits = limits
	if limits.PacketSize() == 0 {
		return out.failed(ErrDatagramTooSmall)
	}
	return out
}

// failed records the error of a probe and its reason.
func (p TransportProbe) failed(err error) TransportProbe {
	p.Err = err
	p.Failure = failureOf(err)
	return p
}

// failureOf classifies the error of a probe.
func failureOf(err error) Failure {
	if rejected, ok := errors.AsType[*rejectedError](err); ok {
		return rejectionFailure(rejected.status)
	}
	if handshake, ok := errors.AsType[*handshakeError](err); ok {
		switch handshake.status {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return FailureUnavailable
		default:
			return FailureNotRelay
		}
	}
	if errors.Is(err, ErrDatagramTooSmall) {
		return FailureDatagramsTooSmall
	}
	if errors.Is(err, errNotRelay) {
		return FailureNotRelay
	}
	if certificateError(err) {
		return FailureCertificate
	}
	if notRelayError(err) {
		return FailureNotRelay
	}

	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return FailureAddress
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return FailureRefused
	}
	if timeoutError(err) {
		return FailureTimeout
	}
	return FailureOther
}

// rejectionFailure classifies a status the relay refused a registration with.
func rejectionFailure(status relayv1.RelayStatus) Failure {
	switch status {
	case relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED:
		return FailureUnauthorized
	case relayv1.RelayStatus_RELAY_STATUS_QUOTA_EXCEEDED:
		return FailureQuota
	case relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE, relayv1.RelayStatus_RELAY_STATUS_SHUTTING_DOWN:
		return FailureUnavailable
	case relayv1.RelayStatus_RELAY_STATUS_DATAGRAMS_REQUIRED:
		return FailureDatagramsTooSmall
	default:
		return FailureOther
	}
}

// certificateError reports a certificate the device did not accept.
func certificateError(err error) bool {
	_, verification := errors.AsType[*tls.CertificateVerificationError](err)
	_, authority := errors.AsType[x509.UnknownAuthorityError](err)
	_, hostname := errors.AsType[x509.HostnameError](err)
	_, invalid := errors.AsType[x509.CertificateInvalidError](err)
	return verification || authority || hostname || invalid
}

// notRelayError reports a server that answered in another protocol.
func notRelayError(err error) bool {
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*quic.VersionNegotiationError](err); ok {
		return true
	}
	transport, ok := errors.AsType[*quic.TransportError](err)
	return ok && transport.ErrorCode == noApplicationProtocol
}

// timeoutError reports an attempt that ran out of time.
func timeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	_, handshake := errors.AsType[*quic.HandshakeTimeoutError](err)
	_, idle := errors.AsType[*quic.IdleTimeoutError](err)
	if handshake || idle {
		return true
	}
	netErr, ok := errors.AsType[net.Error](err)
	return ok && netErr.Timeout()
}
