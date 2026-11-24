package api

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// errRoleRequired reports a pairing window without a role to propose.
var errRoleRequired = errors.New("role must be personal or friends")

// OpenPairingWindow opens a pairing window that proposes role to the devices it
// pairs with.
func (s *Service) OpenPairingWindow(_ context.Context, req *connect.Request[localv1.OpenPairingWindowRequest]) (*connect.Response[localv1.OpenPairingWindowResponse], error) {
	role := roleFromProto(req.Msg.GetRole())
	if role != model.RolePersonal && role != model.RoleFriends {
		return nil, connect.NewError(connect.CodeInvalidArgument, errRoleRequired)
	}
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	if err := s.node.OpenPairing(role); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.OpenPairingWindowResponse{Window: s.window()}), nil
}

// ExtendPairingWindow restarts the auto-close timer of the pairing window.
func (s *Service) ExtendPairingWindow(context.Context, *connect.Request[localv1.ExtendPairingWindowRequest]) (*connect.Response[localv1.ExtendPairingWindowResponse], error) {
	if err := s.node.ExtendPairing(); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ExtendPairingWindowResponse{Window: s.window()}), nil
}

// ClosePairingWindow closes the pairing window.
func (s *Service) ClosePairingWindow(context.Context, *connect.Request[localv1.ClosePairingWindowRequest]) (*connect.Response[localv1.ClosePairingWindowResponse], error) {
	if err := s.node.ClosePairing(); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ClosePairingWindowResponse{}), nil
}

// ListPairingCandidates returns the discovered devices that are open for
// pairing.
func (s *Service) ListPairingCandidates(context.Context, *connect.Request[localv1.ListPairingCandidatesRequest]) (*connect.Response[localv1.ListPairingCandidatesResponse], error) {
	candidates := s.node.PairingCandidates()
	out := make([]*localv1.PairingCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, &localv1.PairingCandidate{
			Addr:            candidate.Addr,
			Txt:             candidate.TXT,
			FirstSeenUnixMs: unixMilli(candidate.FirstSeen),
		})
	}
	return connect.NewResponse(&localv1.ListPairingCandidatesResponse{Candidates: out}), nil
}

// StartSas starts a pairing exchange with a candidate. The exchange runs in the
// background and reports the SAS with the pairing request event.
func (s *Service) StartSas(_ context.Context, req *connect.Request[localv1.StartSasRequest]) (*connect.Response[localv1.StartSasResponse], error) {
	raw := strings.TrimSpace(req.Msg.GetAddr())
	var addr string
	if strings.HasPrefix(raw, pairing.RelayAddrPrefix) {
		if !pairing.IsRelayAddr(raw) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid relay address"))
		}
		addr = raw
	} else {
		var addrErr *connect.Error
		addr, addrErr = addrPort(raw)
		if addrErr != nil {
			return nil, addrErr
		}
	}
	if err := s.node.StartPairing(addr); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.StartSasResponse{}), nil
}

// ConfirmSas accepts the pairing request of the device.
func (s *Service) ConfirmSas(ctx context.Context, req *connect.Request[localv1.ConfirmSasRequest]) (*connect.Response[localv1.ConfirmSasResponse], error) {
	id, err := deviceID(req.Msg.GetDeviceId())
	if err != nil {
		return nil, err
	}
	if err := s.node.ConfirmPairing(ctx, id); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ConfirmSasResponse{}), nil
}

// RejectSas refuses the pairing request of the device.
func (s *Service) RejectSas(ctx context.Context, req *connect.Request[localv1.RejectSasRequest]) (*connect.Response[localv1.RejectSasResponse], error) {
	id, err := deviceID(req.Msg.GetDeviceId())
	if err != nil {
		return nil, err
	}
	if err := s.node.RejectPairing(ctx, id); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.RejectSasResponse{}), nil
}

// GetPairingQr returns the code of the open pairing window. The code carries a
// one time token, so it is meant for the screen of this device only. The addrs
// of the request are the addresses of the pairing listener that the scanning
// device can reach; they replace the addresses of the local interfaces.
func (s *Service) GetPairingQr(_ context.Context, req *connect.Request[localv1.GetPairingQrRequest]) (*connect.Response[localv1.GetPairingQrResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	code, err := s.node.PairingQR(req.Msg.GetAddrs()...)
	if err != nil {
		return nil, errorOf(err)
	}
	uri, err := code.URI()
	if err != nil {
		return nil, errorOf(err)
	}
	deadline, _ := s.node.PairingDeadline()
	return connect.NewResponse(&localv1.GetPairingQrResponse{
		Qr: &localv1.PairingQr{Uri: uri, ExpiresAtUnixMs: unixMilli(deadline)},
	}), nil
}

// ScanPairingQr pairs this device with the device of a pairing code. The
// exchange runs in the background and reports through the events.
func (s *Service) ScanPairingQr(_ context.Context, req *connect.Request[localv1.ScanPairingQrRequest]) (*connect.Response[localv1.ScanPairingQrResponse], error) {
	uri := strings.TrimSpace(req.Msg.GetUri())
	if uri == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the pairing code is empty"))
	}
	if err := s.node.ScanPairingQR(uri); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ScanPairingQrResponse{}), nil
}

// window returns the state of the pairing window.
func (s *Service) window() *localv1.PairingWindow {
	deadline, open := s.node.PairingDeadline()
	if !open {
		return &localv1.PairingWindow{}
	}
	window := &localv1.PairingWindow{Open: true, ExpiresAtUnixMs: unixMilli(deadline)}
	if addr := s.node.PairingAddr(); addr != nil {
		window.Addr = addr.String()
	}
	return window
}
