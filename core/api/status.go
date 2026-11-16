package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// maxDeviceNameLength bounds the display name of the device in bytes.
const maxDeviceNameLength = 128

// maxAddrLength bounds one peer or candidate address in bytes.
const maxAddrLength = 255

// deviceID parses the device ID of a request.
func deviceID(value string) (model.DeviceID, *connect.Error) {
	id, err := model.ParseDeviceID(value)
	if err != nil {
		return model.DeviceID{}, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if id.IsZero() {
		return model.DeviceID{}, connect.NewError(connect.CodeInvalidArgument, errors.New("device id is empty"))
	}
	return id, nil
}

// deviceName validates and trims a display name.
func deviceName(value string) (string, *connect.Error) {
	name := strings.TrimSpace(value)
	switch {
	case name == "":
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("device name is empty"))
	case !utf8.ValidString(name):
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("device name is not valid UTF-8"))
	case len(name) > maxDeviceNameLength:
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("device name is longer than %d bytes", maxDeviceNameLength))
	}
	return name, nil
}

// clipText validates the text of a SetClip request.
func clipText(value string) *connect.Error {
	switch {
	case value == "":
		return connect.NewError(connect.CodeInvalidArgument, errors.New("text is empty"))
	case !utf8.ValidString(value):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("text is not valid UTF-8"))
	case len(value) > clip.MaxInlineText:
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("text is longer than %d bytes", clip.MaxInlineText))
	}
	return nil
}

// addrPort validates a host:port address with a numeric port. An IPv6 host is
// written in brackets.
func addrPort(value string) (string, *connect.Error) {
	addr := strings.TrimSpace(value)
	switch {
	case addr == "":
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("address is empty"))
	case len(addr) > maxAddrLength:
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("address is longer than %d bytes", maxAddrLength))
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, err)
	}
	if host == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("address has no host"))
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("address %q has no port between 1 and 65535", addr))
	}
	return addr, nil
}

// GetStatus reports the version and the runtime state of the daemon.
func (s *Service) GetStatus(ctx context.Context, _ *connect.Request[localv1.GetStatusRequest]) (*connect.Response[localv1.GetStatusResponse], error) {
	status := &localv1.GetStatusResponse{
		Version:    s.version,
		DeviceName: s.name,
		Running:    s.running(),
	}
	if !status.Running {
		return connect.NewResponse(status), nil
	}
	name, err := s.deviceNameValue(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	status.DeviceName = name
	status.DeviceId = s.node.DeviceID().String()
	status.Addrs = s.node.ListenAddrs()

	status.Network = networkToProto(s.node.Network())
	if s.lanBlocked != nil {
		status.LocalNetworkBlocked = s.lanBlocked()
	}

	pause, err := s.pauseState(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	status.Pause = pause

	devices, err := s.devices(ctx, false)
	if err != nil {
		return nil, errorOf(err)
	}
	status.Devices = devices

	on, until, err := s.node.FriendsDiscovery(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	status.FriendsDiscovery = friendsDiscoveryToProto(on, until)
	return connect.NewResponse(status), nil
}

// WatchEvents streams the node events until the client disconnects.
func (s *Service) WatchEvents(ctx context.Context, _ *connect.Request[localv1.WatchEventsRequest], stream *connect.ServerStream[localv1.WatchEventsResponse]) error {
	if err := s.requireRunning(); err != nil {
		return errorOf(err)
	}

	subscriber := s.node.Subscribe()
	defer subscriber.Close()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-subscriber.Events():
			if !ok {
				return nil
			}
			response := &localv1.WatchEventsResponse{Event: eventToProto(event)}
			if err := stream.Send(response); err != nil {
				return err
			}
		}
	}
}
