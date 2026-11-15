package api

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
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
	addr := req.Msg.GetAddr()
	if err := candidateAddr(addr); err != nil {
		return nil, err
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
