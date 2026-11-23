package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/relay"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// maxRelayURLLength bounds the relay address, control characters included.
const maxRelayURLLength = trust.MaxRelayURLLength

// GetRelay returns the relay access of the Personal group and the state of the
// client.
func (s *Service) GetRelay(ctx context.Context, _ *connect.Request[localv1.GetRelayRequest]) (*connect.Response[localv1.GetRelayResponse], error) {
	current, err := s.relay(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.GetRelayResponse{Relay: current}), nil
}

// SetRelay stores the relay access of the group. The other Personal devices
// receive it with the group data.
func (s *Service) SetRelay(ctx context.Context, req *connect.Request[localv1.SetRelayRequest]) (*connect.Response[localv1.SetRelayResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	settings, invalid := s.relaySettings(ctx, req.Msg.GetUrl(), req.Msg.Token)
	if invalid != nil {
		return nil, invalid
	}
	if err := s.node.SetRelay(ctx, settings); err != nil {
		return nil, errorOf(err)
	}
	if value := req.Msg.FilesOnRequest; value != nil {
		if err := s.node.SetRelayFilesOnRequest(ctx, *value); err != nil {
			return nil, errorOf(err)
		}
	}

	current, err := s.relay(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.SetRelayResponse{Relay: current}), nil
}

// RelayOff turns the relay off for the group.
func (s *Service) RelayOff(ctx context.Context, _ *connect.Request[localv1.RelayOffRequest]) (*connect.Response[localv1.RelayOffResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	if err := s.node.SetRelay(ctx, trust.RelaySettings{}); err != nil {
		return nil, errorOf(err)
	}

	current, err := s.relay(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.RelayOffResponse{Relay: current}), nil
}

// relay reads the relay access and the state of the client.
func (s *Service) relay(ctx context.Context) (*localv1.Relay, error) {
	if err := s.requireRunning(); err != nil {
		return nil, err
	}
	settings, changedAt, err := s.node.RelaySettings(ctx)
	if err != nil {
		return nil, err
	}
	return relayToProto(settings, changedAt, s.node.RelayStatus(), s.node.RelayFilesOnRequest()), nil
}

// TestRelay checks that a relay accepts this device over each transport. An
// empty address and an absent token stand for the stored ones.
func (s *Service) TestRelay(ctx context.Context, req *connect.Request[localv1.TestRelayRequest]) (*connect.Response[localv1.TestRelayResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	url := req.Msg.GetUrl()
	if strings.TrimSpace(url) == "" {
		stored, _, err := s.node.RelaySettings(ctx)
		if err != nil {
			return nil, errorOf(err)
		}
		url = stored.URL
	}
	settings, invalid := s.relaySettings(ctx, url, req.Msg.Token)
	if invalid != nil {
		return nil, invalid
	}

	result, err := s.node.CheckRelay(ctx, settings)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.TestRelayResponse{
		Quic:      relayCheckToProto(result.QUIC),
		Websocket: relayCheckToProto(result.WebSocket),
	}), nil
}

// relaySettings validates a relay address and a token. An absent token stands
// for the stored one.
func (s *Service) relaySettings(ctx context.Context, rawURL string, rawToken *string) (trust.RelaySettings, *connect.Error) {
	url := strings.TrimSpace(rawURL)
	switch {
	case url == "":
		return trust.RelaySettings{}, invalidRelay("a relay address is required")
	case len(url) > maxRelayURLLength:
		return trust.RelaySettings{}, invalidRelay(fmt.Sprintf("the relay address is longer than %d bytes", maxRelayURLLength))
	}
	if err := relay.ValidateURL(url); err != nil {
		return trust.RelaySettings{}, invalidRelay(err.Error())
	}

	var token string
	if rawToken == nil {
		stored, _, err := s.node.RelaySettings(ctx)
		if err != nil {
			return trust.RelaySettings{}, errorOf(err)
		}
		token = stored.Token
	} else {
		token = strings.TrimSpace(*rawToken)
	}
	switch {
	case token == "":
		return trust.RelaySettings{}, invalidRelay("a relay access token is required")
	case len(token) > trust.MaxRelayTokenLength:
		return trust.RelaySettings{}, invalidRelay(fmt.Sprintf("the access token is longer than %d bytes", trust.MaxRelayTokenLength))
	}
	return trust.RelaySettings{URL: url, Token: token}, nil
}

// invalidRelay reports a relay setting that cannot be used.
func invalidRelay(message string) *connect.Error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New(message))
}

// relayToProto converts the relay access and the state of the client. The token
// is never part of the response.
func relayToProto(settings trust.RelaySettings, changedAt time.Time, status relay.Status, filesOnRequest bool) *localv1.Relay {
	return &localv1.Relay{
		Url:             settings.URL,
		TokenSet:        settings.Token != "",
		FilesOnRequest:  filesOnRequest,
		ChangedAtUnixMs: unixMilli(changedAt),
		Connected:       status.Connected,
		Transport:       relayTransportToProto(status.Transport),
		ProtocolVersion: status.ProtocolVersion,
		ProtocolNewer:   status.ProtocolNewer,
	}
}

// relayTransportToProto converts the transport of the relay client.
func relayTransportToProto(transport relay.Transport) localv1.RelayTransport {
	switch transport {
	case relay.TransportQUIC:
		return localv1.RelayTransport_RELAY_TRANSPORT_QUIC
	case relay.TransportWebSocket:
		return localv1.RelayTransport_RELAY_TRANSPORT_WSS
	default:
		return localv1.RelayTransport_RELAY_TRANSPORT_NONE
	}
}

// relayCheckToProto converts the outcome of a check of one transport.
func relayCheckToProto(probe relay.TransportProbe) *localv1.RelayCheck {
	out := &localv1.RelayCheck{
		Transport: relayTransportToProto(probe.Transport),
		Ok:        probe.OK(),
		Failure:   relayFailureToProto(probe.Failure),
	}
	if probe.OK() {
		out.LatencyMs = probe.Latency.Milliseconds()
	}
	if probe.Err != nil {
		out.Detail = probe.Err.Error()
	}
	return out
}

// relayFailureToProto converts the reason a check failed.
func relayFailureToProto(failure relay.Failure) localv1.RelayCheckFailure {
	switch failure {
	case relay.FailureNone:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_NONE
	case relay.FailureAddress:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_ADDRESS
	case relay.FailureTimeout:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_TIMEOUT
	case relay.FailureRefused:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_REFUSED
	case relay.FailureCertificate:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_CERTIFICATE
	case relay.FailureNotRelay:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_NOT_RELAY
	case relay.FailureUnavailable:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_UNAVAILABLE
	case relay.FailureUnauthorized:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_UNAUTHORIZED
	case relay.FailureQuota:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_QUOTA_EXCEEDED
	case relay.FailureDatagramsTooSmall:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_DATAGRAMS_TOO_SMALL
	default:
		return localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_OTHER
	}
}
