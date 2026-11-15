package api

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// errDevicesUnavailable reports the device management calls the core does not
// serve yet.
var errDevicesUnavailable = errors.New("api: device management is not implemented")

// ListDevices returns the paired devices. The call is not implemented yet and
// the clients read the devices from GetStatus.
func (s *Service) ListDevices(context.Context, *connect.Request[localv1.ListDevicesRequest]) (*connect.Response[localv1.ListDevicesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errDevicesUnavailable)
}

// RenameDevice changes the display name of a paired device.
func (s *Service) RenameDevice(context.Context, *connect.Request[localv1.RenameDeviceRequest]) (*connect.Response[localv1.RenameDeviceResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errDevicesUnavailable)
}

// RevokeDevice revokes a paired device.
func (s *Service) RevokeDevice(context.Context, *connect.Request[localv1.RevokeDeviceRequest]) (*connect.Response[localv1.RevokeDeviceResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errDevicesUnavailable)
}

// SetDeviceConfirmation sets the confirmation flag of this device or of a
// paired device. The flag of a paired device is changed by that device, so the
// request travels there as a signed policy.
func (s *Service) SetDeviceConfirmation(ctx context.Context, req *connect.Request[localv1.SetDeviceConfirmationRequest]) (*connect.Response[localv1.SetDeviceConfirmationResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, err := confirmationTarget(s.node.DeviceID(), req.Msg)
	if err != nil {
		return nil, errorOf(err)
	}
	if err := s.node.SetPeerRequireConfirmation(ctx, id, req.Msg.GetRequire()); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.SetDeviceConfirmationResponse{}), nil
}

// confirmationTarget returns the device whose flag the request changes. The
// local device is the target when self is set.
func confirmationTarget(local model.DeviceID, req *localv1.SetDeviceConfirmationRequest) (model.DeviceID, error) {
	if req.GetSelf() {
		if req.GetDeviceId() != "" {
			return model.DeviceID{}, fmt.Errorf("%w: self and device id", trust.ErrInvalidPeer)
		}
		return local, nil
	}
	if req.GetDeviceId() == "" {
		return model.DeviceID{}, fmt.Errorf("%w: no device selected", trust.ErrInvalidPeer)
	}
	id, err := model.ParseDeviceID(req.GetDeviceId())
	if err != nil {
		return model.DeviceID{}, fmt.Errorf("%w: device id", trust.ErrInvalidPeer)
	}
	return id, nil
}
