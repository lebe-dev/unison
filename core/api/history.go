package api

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/model"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// Limits of the history requests.
const (
	// maxPathLength bounds the destination path of a history download.
	maxPathLength = 4096
	// defaultThumbnailSide is the side of a thumbnail when the request sets
	// none.
	defaultThumbnailSide = 256
)

// ListHistory returns the live items of the history, newest first.
func (s *Service) ListHistory(ctx context.Context, req *connect.Request[localv1.ListHistoryRequest]) (*connect.Response[localv1.ListHistoryResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	filter := history.ListFilter{
		Kind:  kindFromProto(req.Msg.GetKind()),
		Limit: int(req.Msg.GetLimit()),
	}
	if device := req.Msg.GetDeviceId(); device != "" {
		id, err := model.ParseDeviceID(device)
		if err != nil {
			return nil, errorOf(fmt.Errorf("%w: device id", history.ErrInvalidItem))
		}
		filter.Origin = id
	}

	items, err := s.node.History().List(ctx, filter)
	if err != nil {
		return nil, errorOf(err)
	}
	out := make([]*localv1.HistoryItem, 0, len(items))
	for _, item := range items {
		out = append(out, historyItemToProto(item))
	}
	return connect.NewResponse(&localv1.ListHistoryResponse{Items: out}), nil
}

// GetHistoryItem returns one live item.
func (s *Service) GetHistoryItem(ctx context.Context, req *connect.Request[localv1.GetHistoryItemRequest]) (*connect.Response[localv1.GetHistoryItemResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, err := historyID(req.Msg.GetId())
	if err != nil {
		return nil, errorOf(err)
	}
	item, err := s.node.History().Get(ctx, id)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.GetHistoryItemResponse{Item: historyItemToProto(item)}), nil
}

// GetHistoryThumbnail returns a scaled down copy of the image of one item. The
// image is decoded here, so the app never hands the payload of a peer to the
// image decoders of the platform.
func (s *Service) GetHistoryThumbnail(ctx context.Context, req *connect.Request[localv1.GetHistoryThumbnailRequest]) (*connect.Response[localv1.GetHistoryThumbnailResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, err := historyID(req.Msg.GetId())
	if err != nil {
		return nil, errorOf(err)
	}
	item, err := s.node.History().Get(ctx, id)
	if err != nil {
		return nil, errorOf(err)
	}
	pixels, err := s.node.Clip().Thumbnail(ctx, item.ClipItem, thumbnailSide(req.Msg.GetMaxSide()))
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.GetHistoryThumbnailResponse{
		Width:  uint32(pixels.Width),  //nolint:gosec // The side is bounded by clip.MaxThumbnailSide.
		Height: uint32(pixels.Height), //nolint:gosec // The side is bounded by clip.MaxThumbnailSide.
		Rgba:   pixels.RGBA,
	}), nil
}

// thumbnailSide returns the requested side of a thumbnail within the limits.
func thumbnailSide(requested uint32) int {
	switch {
	case requested == 0:
		return defaultThumbnailSide
	case requested > clip.MaxThumbnailSide:
		return clip.MaxThumbnailSide
	default:
		return int(requested)
	}
}

// DeleteHistoryItem marks one item as deleted on every device.
func (s *Service) DeleteHistoryItem(ctx context.Context, req *connect.Request[localv1.DeleteHistoryItemRequest]) (*connect.Response[localv1.DeleteHistoryItemResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, err := historyID(req.Msg.GetId())
	if err != nil {
		return nil, errorOf(err)
	}
	if err := s.node.History().Delete(ctx, id); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.DeleteHistoryItemResponse{}), nil
}

// ClearHistory removes every live item of the history. The tombstones reach
// the other Personal devices, so the items disappear there too.
func (s *Service) ClearHistory(ctx context.Context, _ *connect.Request[localv1.ClearHistoryRequest]) (*connect.Response[localv1.ClearHistoryResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	removed, err := s.node.History().Clear(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ClearHistoryResponse{Removed: uint64(removed)}), nil
}

// FetchHistoryItem downloads the payloads of one item and writes them to the
// requested path.
func (s *Service) FetchHistoryItem(ctx context.Context, req *connect.Request[localv1.FetchHistoryItemRequest]) (*connect.Response[localv1.FetchHistoryItemResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	id, err := historyID(req.Msg.GetId())
	if err != nil {
		return nil, errorOf(err)
	}
	path := req.Msg.GetPath()
	if len(path) > maxPathLength {
		return nil, errorOf(fmt.Errorf("%w: path is too long", history.ErrInvalidItem))
	}

	item, files, err := s.node.History().Fetch(ctx, id, path)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.FetchHistoryItemResponse{
		Item:  historyItemToProto(item),
		Files: files,
	}), nil
}

// CopyHistoryItem writes one item back to the local clipboard.
func (s *Service) CopyHistoryItem(ctx context.Context, req *connect.Request[localv1.CopyHistoryItemRequest]) (*connect.Response[localv1.CopyHistoryItemResponse], error) {
	if err := s.applyHistoryItem(ctx, req.Msg.GetId()); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.CopyHistoryItemResponse{}), nil
}

// ApplyHistoryItem writes one item to the local clipboard. It is how a user
// applies an item that waits for confirmation.
func (s *Service) ApplyHistoryItem(ctx context.Context, req *connect.Request[localv1.ApplyHistoryItemRequest]) (*connect.Response[localv1.ApplyHistoryItemResponse], error) {
	if err := s.applyHistoryItem(ctx, req.Msg.GetId()); err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ApplyHistoryItemResponse{}), nil
}

// applyHistoryItem writes a recorded item to the clipboard through the
// pipeline, which keeps the write from coming back as a new item.
func (s *Service) applyHistoryItem(ctx context.Context, reference string) error {
	if err := s.requireRunning(); err != nil {
		return err
	}
	id, err := historyID(reference)
	if err != nil {
		return err
	}
	item, err := s.node.History().Get(ctx, id)
	if err != nil {
		return err
	}
	return s.node.Clip().ApplyHistory(ctx, item.ClipItem)
}

// historyID parses the item id of a request.
func historyID(id string) (model.ClipID, error) {
	parsed, err := model.ParseClipID(id)
	if err != nil {
		return model.ClipID{}, fmt.Errorf("%w: item id", history.ErrInvalidItem)
	}
	return parsed, nil
}

// historyItemToProto converts one history entry.
func historyItemToProto(item history.Item) *localv1.HistoryItem {
	return &localv1.HistoryItem{
		Item:     clipItemToProto(item.ClipItem),
		Complete: item.Complete,
	}
}
