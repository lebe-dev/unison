package api

import (
	"context"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/mediate"
	"github.com/lebe-dev/unison/core/model"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// StartMediatedPair introduces two online devices of this one to each other.
func (s *Service) StartMediatedPair(ctx context.Context, req *connect.Request[localv1.StartMediatedPairRequest]) (*connect.Response[localv1.StartMediatedPairResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	peerA, err := deviceID(req.Msg.GetPeerA())
	if err != nil {
		return nil, err
	}
	peerB, idErr := deviceID(req.Msg.GetPeerB())
	if idErr != nil {
		return nil, idErr
	}
	role := roleFromProto(req.Msg.GetRole())
	if role != model.RolePersonal && role != model.RoleFriends {
		return nil, errorOf(mediate.ErrInvalidOffer)
	}
	exchange, startErr := s.node.StartMediatedPair(ctx, peerA, peerB, role)
	if startErr != nil {
		return nil, errorOf(startErr)
	}
	return connect.NewResponse(&localv1.StartMediatedPairResponse{Pair: mediatedPairToProto(exchange)}), nil
}

// ConfirmMediatedPair accepts an incoming offer.
func (s *Service) ConfirmMediatedPair(ctx context.Context, req *connect.Request[localv1.ConfirmMediatedPairRequest]) (*connect.Response[localv1.ConfirmMediatedPairResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	sessionID := req.Msg.GetSessionId()
	if err := s.node.ConfirmMediatedPair(ctx, sessionID, req.Msg.GetEnableFriendsDiscovery()); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ConfirmMediatedPairResponse{Pair: s.mediatedPair(sessionID)}), nil
}

// RejectMediatedPair declines an incoming offer.
func (s *Service) RejectMediatedPair(ctx context.Context, req *connect.Request[localv1.RejectMediatedPairRequest]) (*connect.Response[localv1.RejectMediatedPairResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	sessionID := req.Msg.GetSessionId()
	if err := s.node.RejectMediatedPair(ctx, sessionID); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.RejectMediatedPairResponse{Pair: s.mediatedPair(sessionID)}), nil
}

// CancelMediatedPair ends an exchange.
func (s *Service) CancelMediatedPair(ctx context.Context, req *connect.Request[localv1.CancelMediatedPairRequest]) (*connect.Response[localv1.CancelMediatedPairResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	sessionID := req.Msg.GetSessionId()
	if err := s.node.CancelMediatedPair(ctx, sessionID); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.CancelMediatedPairResponse{Pair: s.mediatedPair(sessionID)}), nil
}

// ListMediatedPairs returns the active exchanges.
func (s *Service) ListMediatedPairs(_ context.Context, _ *connect.Request[localv1.ListMediatedPairsRequest]) (*connect.Response[localv1.ListMediatedPairsResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	exchanges := s.node.MediatedPairs()
	pairs := make([]*localv1.MediatedPair, 0, len(exchanges))
	for _, exchange := range exchanges {
		pairs = append(pairs, mediatedPairToProto(exchange))
	}
	return connect.NewResponse(&localv1.ListMediatedPairsResponse{Pairs: pairs}), nil
}

// mediatedPair returns the exchange with the identifier, or an empty one when
// it completed and left the list.
func (s *Service) mediatedPair(sessionID string) *localv1.MediatedPair {
	for _, exchange := range s.node.MediatedPairs() {
		if exchange.SessionID == sessionID {
			return mediatedPairToProto(exchange)
		}
	}
	return &localv1.MediatedPair{SessionId: sessionID}
}

// mediatedPairToProto converts one exchange.
func mediatedPairToProto(exchange mediate.Exchange) *localv1.MediatedPair {
	return &localv1.MediatedPair{
		SessionId:              exchange.SessionID,
		Direction:              mediatedDirectionToProto(exchange.Direction),
		State:                  mediatedStateToProto(exchange.State),
		Role:                   roleToProto(exchange.Role),
		Mediator:               mediatedDeviceToProto(exchange.Mediator),
		PeerA:                  mediatedDeviceToProto(exchange.PeerA),
		PeerB:                  mediatedDeviceToProto(exchange.PeerB),
		Peer:                   mediatedDeviceToProto(exchange.Peer),
		Sas:                    sasToProto(exchange.SAS),
		AlreadyPaired:          exchange.AlreadyPaired,
		ExpiresAtUnixMs:        unixMilli(exchange.ExpiresAt),
		CancelReason:           mediatedCancelReasonToProto(exchange.CancelReason),
		EnableFriendsDiscovery: exchange.EnableFriendsDiscovery,
	}
}

// mediatedDeviceToProto converts one endpoint of an exchange.
func mediatedDeviceToProto(device mediate.Device) *localv1.MediatedDevice {
	if device.ID.IsZero() {
		return nil
	}
	return &localv1.MediatedDevice{
		DeviceId: device.ID.String(),
		Name:     device.Name,
		Platform: platformToProto(device.Platform),
	}
}

// mediatedDirectionToProto converts the side of an exchange.
func mediatedDirectionToProto(direction mediate.Direction) localv1.MediatedPairDirection {
	switch direction {
	case mediate.DirectionMediator:
		return localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_MEDIATOR
	case mediate.DirectionIncoming:
		return localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_INCOMING
	default:
		return localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_UNSPECIFIED
	}
}

// mediatedStateToProto converts the state of an exchange.
func mediatedStateToProto(state mediate.State) localv1.MediatedPairState {
	switch state {
	case mediate.StateWaiting:
		return localv1.MediatedPairState_MEDIATED_PAIR_STATE_WAITING
	case mediate.StateReady:
		return localv1.MediatedPairState_MEDIATED_PAIR_STATE_READY
	case mediate.StateAwaitingFinish:
		return localv1.MediatedPairState_MEDIATED_PAIR_STATE_AWAITING_FINISH
	case mediate.StateCompleted:
		return localv1.MediatedPairState_MEDIATED_PAIR_STATE_COMPLETED
	case mediate.StateCanceled:
		return localv1.MediatedPairState_MEDIATED_PAIR_STATE_CANCELED
	default:
		return localv1.MediatedPairState_MEDIATED_PAIR_STATE_UNSPECIFIED
	}
}

// mediatedCancelReasonToProto converts the reason of an ended exchange.
func mediatedCancelReasonToProto(reason mediate.CancelReason) localv1.MediatedPairCancelReason {
	switch reason {
	case mediate.CancelReasonUserRejected:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_USER_REJECTED
	case mediate.CancelReasonMediatorCanceled:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_MEDIATOR_CANCELED
	case mediate.CancelReasonExpired:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_EXPIRED
	case mediate.CancelReasonRevoked:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_REVOKED
	case mediate.CancelReasonRoleMismatch:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_ROLE_MISMATCH
	case mediate.CancelReasonAlreadyPaired:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_ALREADY_PAIRED
	case mediate.CancelReasonBusy:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_BUSY
	case mediate.CancelReasonPeerOffline:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_PEER_OFFLINE
	case mediate.CancelReasonFailed:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_FAILED
	default:
		return localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_UNSPECIFIED
	}
}
