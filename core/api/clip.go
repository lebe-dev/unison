package api

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/ports"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// errLocalClipUnavailable reports SetClip without a way into the pipeline.
var errLocalClipUnavailable = errors.New("api: local clipboard submit is not configured")

// GetClip returns the most recent item of the clipboard history.
func (s *Service) GetClip(_ context.Context, _ *connect.Request[localv1.GetClipRequest]) (*connect.Response[localv1.GetClipResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	response := &localv1.GetClipResponse{}
	if item, ok := s.node.Clip().Last(); ok {
		response.Item = clipItemToProto(item)
	}
	return connect.NewResponse(response), nil
}

// SetClip copies local text to the clipboard and announces it to the peers.
func (s *Service) SetClip(ctx context.Context, req *connect.Request[localv1.SetClipRequest]) (*connect.Response[localv1.SetClipResponse], error) {
	if s.localClip == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errLocalClipUnavailable)
	}
	text := req.Msg.GetText()
	if err := clipText(text); err != nil {
		return nil, err
	}
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	content := ports.Content{Representations: []ports.Representation{
		{MIME: clip.MIMETextPlain, Data: []byte(text)},
	}}
	item, err := s.localClip.Submit(ctx, content)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.SetClipResponse{Item: clipItemToProto(item)}), nil
}
