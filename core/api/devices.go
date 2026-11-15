package api

import (
	"context"
	"errors"

	"connectrpc.com/connect"

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

// SetDeviceConfirmation sets the confirmation flag of a paired device.
func (s *Service) SetDeviceConfirmation(context.Context, *connect.Request[localv1.SetDeviceConfirmationRequest]) (*connect.Response[localv1.SetDeviceConfirmationResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errDevicesUnavailable)
}
