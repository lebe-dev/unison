package api

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// ListDevices returns the paired devices with their live connection state.
func (s *Service) ListDevices(ctx context.Context, req *connect.Request[localv1.ListDevicesRequest]) (*connect.Response[localv1.ListDevicesResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	devices, err := s.devices(ctx, req.Msg.GetIncludeRevoked())
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ListDevicesResponse{Devices: devices}), nil
}

// RenameDevice changes the display name of a paired device. The name of a peer
// is local to this device and does not travel to it. The name of this device is
// stored and announced to the peers on the next handshake.
func (s *Service) RenameDevice(ctx context.Context, req *connect.Request[localv1.RenameDeviceRequest]) (*connect.Response[localv1.RenameDeviceResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, idErr := deviceID(req.Msg.GetDeviceId())
	if idErr != nil {
		return nil, idErr
	}
	name, nameErr := deviceName(req.Msg.GetName())
	if nameErr != nil {
		return nil, nameErr
	}

	if id == s.node.DeviceID() {
		if err := s.node.SetDeviceName(ctx, name); err != nil {
			return nil, errorOf(err)
		}
		return connect.NewResponse(&localv1.RenameDeviceResponse{
			Device: &localv1.Device{DeviceId: id.String(), Name: name, Online: true},
		}), nil
	}

	if err := s.node.Trust().Rename(ctx, id, name); err != nil {
		return nil, errorOf(err)
	}
	peer, err := s.node.Trust().Get(ctx, id)
	if err != nil {
		return nil, errorOf(err)
	}
	state, err := s.node.Peer(ctx, id)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.RenameDeviceResponse{Device: deviceToProto(peer, state)}), nil
}

// RevokeDevice revokes a paired device or, when the local device is named,
// leaves the group. A revoked device is cut off from the devices of the group
// and stays on the deny-list.
func (s *Service) RevokeDevice(ctx context.Context, req *connect.Request[localv1.RevokeDeviceRequest]) (*connect.Response[localv1.RevokeDeviceResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, err := model.ParseDeviceID(req.Msg.GetDeviceId())
	if err != nil {
		return nil, errorOf(fmt.Errorf("%w: device id", trust.ErrInvalidPeer))
	}
	if err := s.node.RevokePeer(ctx, id); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.RevokeDeviceResponse{}), nil
}

// SetDeviceConfirmation sets the confirmation flag of this device or of a
// paired device. The flag of a paired device is changed by that device, so the
// request travels there as a signed policy; the local policy for the peer is
// recorded at the same time.
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

// SetPeerAddress stores an address of a paired device that the user entered and
// connects to it. The address is tried before the discovered and cached ones,
// so a device is reachable on a network where discovery does not work.
func (s *Service) SetPeerAddress(ctx context.Context, req *connect.Request[localv1.SetPeerAddressRequest]) (*connect.Response[localv1.SetPeerAddressResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, idErr := deviceID(req.Msg.GetDeviceId())
	if idErr != nil {
		return nil, idErr
	}
	addr, addrErr := addrPort(req.Msg.GetAddr())
	if addrErr != nil {
		return nil, addrErr
	}
	peer, err := s.node.Trust().Get(ctx, id)
	if err != nil {
		return nil, errorOf(err)
	}
	if peer.Revoked {
		return nil, errorOf(trust.ErrPeerRevoked)
	}
	if err := s.node.SetPeerAddress(ctx, id, addr); err != nil {
		return nil, errorOf(err)
	}
	state, err := s.node.Peer(ctx, id)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.SetPeerAddressResponse{Device: deviceToProto(peer, state)}), nil
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
