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

	settings, invalid := s.relaySettings(ctx, req.Msg)
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

// relaySettings validates the relay access of a request. A request without a
// token keeps the stored one.
func (s *Service) relaySettings(ctx context.Context, req *localv1.SetRelayRequest) (trust.RelaySettings, *connect.Error) {
	url := strings.TrimSpace(req.GetUrl())
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
	if req.Token == nil {
		stored, _, err := s.node.RelaySettings(ctx)
		if err != nil {
			return trust.RelaySettings{}, errorOf(err)
		}
		token = stored.Token
	} else {
		token = strings.TrimSpace(*req.Token)
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
