package api

import (
	"context"
	"encoding/hex"
	"fmt"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/share"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// ListRequests returns the incoming file transfer requests that wait for a
// decision.
func (s *Service) ListRequests(_ context.Context, _ *connect.Request[localv1.ListRequestsRequest]) (*connect.Response[localv1.ListRequestsResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	records := s.node.Share().Requests()
	requests := make([]*localv1.ShareRequest, 0, len(records))
	for _, record := range records {
		requests = append(requests, requestToProto(record))
	}
	return connect.NewResponse(&localv1.ListRequestsResponse{Requests: requests}), nil
}

// DecideRequest accepts or declines an incoming file transfer request.
func (s *Service) DecideRequest(ctx context.Context, req *connect.Request[localv1.DecideRequestRequest]) (*connect.Response[localv1.DecideRequestResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	id := req.Msg.GetId()
	if err := s.node.Share().Decide(ctx, id, req.Msg.GetAccept()); err != nil {
		return nil, errorOf(err)
	}
	record, err := s.node.Share().Transfer(id)
	if err != nil {
		return nil, errorOf(fmt.Errorf("read the request: %w", err))
	}
	return connect.NewResponse(&localv1.DecideRequestResponse{Request: requestToProto(record)}), nil
}

// requestToProto converts one incoming request.
func requestToProto(record share.Transfer) *localv1.ShareRequest {
	return &localv1.ShareRequest{
		Id:              record.ID,
		DeviceId:        record.Peer.String(),
		DeviceName:      record.PeerName,
		Files:           filesToProto(record.Files),
		TotalBytes:      record.TotalBytes,
		CreatedAtUnixMs: unixMilli(record.CreatedAt),
		ExpiresAtUnixMs: unixMilli(record.ExpiresAt),
	}
}

// filesToProto converts the files of a transfer.
func filesToProto(files []share.File) []*localv1.TransferFile {
	out := make([]*localv1.TransferFile, 0, len(files))
	for _, file := range files {
		out = append(out, &localv1.TransferFile{
			Name:        file.Name,
			Size:        uint64(max(file.Size, 0)),
			Blake3:      hex.EncodeToString(file.Hash[:]),
			MtimeUnixMs: unixMilli(file.MTime),
			MimeType:    file.MIME,
		})
	}
	return out
}
