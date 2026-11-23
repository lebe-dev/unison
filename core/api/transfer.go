package api

import (
	"context"
	"encoding/hex"
	"fmt"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/share"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// SendFiles offers files of this device to the given devices.
func (s *Service) SendFiles(ctx context.Context, req *connect.Request[localv1.SendFilesRequest]) (*connect.Response[localv1.SendFilesResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	paths := req.Msg.GetPaths()
	switch {
	case len(paths) == 0:
		return nil, errorOf(share.ErrNoFiles)
	case len(paths) > share.MaxFiles:
		return nil, errorOf(share.ErrTooManyFiles)
	}
	for _, path := range paths {
		if len(path) > maxPathLength {
			return nil, errorOf(fmt.Errorf("%w: path is too long", share.ErrInvalidFile))
		}
	}

	peers := req.Msg.GetDeviceIds()
	if len(peers) == 0 {
		return nil, errorOf(share.ErrNoPeers)
	}
	devices := make([]model.DeviceID, 0, len(peers))
	for _, value := range peers {
		device, err := deviceID(value)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}

	ids, err := s.node.Share().SendPaths(ctx, paths, devices)
	if err != nil {
		return nil, errorOf(err)
	}
	transfers := make([]*localv1.Transfer, 0, len(ids))
	for _, id := range ids {
		record, err := s.node.Share().Transfer(id)
		if err != nil {
			continue
		}
		transfers = append(transfers, TransferToProto(record))
	}
	return connect.NewResponse(&localv1.SendFilesResponse{Transfers: transfers}), nil
}

// ListTransfers returns the transfers of this device, newest first.
func (s *Service) ListTransfers(_ context.Context, _ *connect.Request[localv1.ListTransfersRequest]) (*connect.Response[localv1.ListTransfersResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	records := s.node.Share().Transfers()
	transfers := make([]*localv1.Transfer, 0, len(records))
	for _, record := range records {
		transfers = append(transfers, TransferToProto(record))
	}
	return connect.NewResponse(&localv1.ListTransfersResponse{Transfers: transfers}), nil
}

// CancelTransfer stops a transfer.
func (s *Service) CancelTransfer(ctx context.Context, req *connect.Request[localv1.CancelTransferRequest]) (*connect.Response[localv1.CancelTransferResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	if err := s.node.Share().Cancel(ctx, req.Msg.GetId()); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.CancelTransferResponse{}), nil
}

// AcceptTransfer accepts an incoming offer that waits for a decision.
func (s *Service) AcceptTransfer(ctx context.Context, req *connect.Request[localv1.AcceptTransferRequest]) (*connect.Response[localv1.AcceptTransferResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	if err := s.node.Share().Accept(ctx, req.Msg.GetId()); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.AcceptTransferResponse{}), nil
}

// RejectTransfer declines an incoming offer that waits for a decision.
func (s *Service) RejectTransfer(ctx context.Context, req *connect.Request[localv1.RejectTransferRequest]) (*connect.Response[localv1.RejectTransferResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	if err := s.node.Share().Reject(ctx, req.Msg.GetId()); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.RejectTransferResponse{}), nil
}

// TransferToProto converts one transfer to its local API form.
func TransferToProto(record share.Transfer) *localv1.Transfer {
	out := &localv1.Transfer{
		Id:              record.ID,
		Direction:       transferDirectionToProto(record.Direction),
		DeviceId:        record.Peer.String(),
		DeviceName:      record.PeerName,
		State:           transferStateToProto(record.State),
		Bytes:           record.Bytes,
		TotalBytes:      record.TotalBytes,
		Paths:           append([]string(nil), record.Paths...),
		Error:           record.Err,
		CreatedAtUnixMs: unixMilli(record.CreatedAt),
	}
	for _, file := range record.Files {
		out.Files = append(out.Files, &localv1.TransferFile{
			Name:        file.Name,
			Size:        uint64(max(file.Size, 0)),
			Blake3:      hex.EncodeToString(file.Hash[:]),
			MtimeUnixMs: unixMilli(file.MTime),
			MimeType:    file.MIME,
		})
	}
	return out
}

// transferDirectionToProto converts a transfer direction.
func transferDirectionToProto(direction share.Direction) localv1.TransferDirection {
	switch direction {
	case share.DirectionOutgoing:
		return localv1.TransferDirection_TRANSFER_DIRECTION_OUTGOING
	case share.DirectionIncoming:
		return localv1.TransferDirection_TRANSFER_DIRECTION_INCOMING
	default:
		return localv1.TransferDirection_TRANSFER_DIRECTION_UNSPECIFIED
	}
}

// transferStateToProto converts a transfer state.
func transferStateToProto(state share.State) localv1.TransferState {
	switch state {
	case share.StatePending:
		return localv1.TransferState_TRANSFER_STATE_PENDING
	case share.StateOffered:
		return localv1.TransferState_TRANSFER_STATE_OFFERED
	case share.StateTransferring:
		return localv1.TransferState_TRANSFER_STATE_TRANSFERRING
	case share.StateDone:
		return localv1.TransferState_TRANSFER_STATE_DONE
	case share.StateDeclined:
		return localv1.TransferState_TRANSFER_STATE_DECLINED
	case share.StateCanceled:
		return localv1.TransferState_TRANSFER_STATE_CANCELED
	case share.StateFailed:
		return localv1.TransferState_TRANSFER_STATE_FAILED
	default:
		return localv1.TransferState_TRANSFER_STATE_UNSPECIFIED
	}
}
