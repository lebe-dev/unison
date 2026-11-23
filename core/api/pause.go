package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// GetPause reports the pause state of the clipboard synchronization.
func (s *Service) GetPause(ctx context.Context, _ *connect.Request[localv1.GetPauseRequest]) (*connect.Response[localv1.GetPauseResponse], error) {
	pause, err := s.pauseState(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.GetPauseResponse{Pause: pause}), nil
}

// SetPause turns the pause of the clipboard synchronization on or off.
func (s *Service) SetPause(ctx context.Context, req *connect.Request[localv1.SetPauseRequest]) (*connect.Response[localv1.SetPauseResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	pipeline := s.node.Clip()
	var err error
	switch req.Msg.GetMode() {
	case localv1.PauseMode_PAUSE_MODE_OFF:
		err = pipeline.Resume(ctx)
	case localv1.PauseMode_PAUSE_MODE_INDEFINITE:
		err = pipeline.Pause(ctx, time.Time{})
	case localv1.PauseMode_PAUSE_MODE_UNTIL:
		until := time.UnixMilli(req.Msg.GetUntilUnixMs())
		if !until.After(s.clock.Now()) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("pause deadline is not in the future"))
		}
		err = pipeline.Pause(ctx, until)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("pause mode is required"))
	}
	if err != nil {
		return nil, errorOf(err)
	}

	pause, err := s.pauseState(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.SetPauseResponse{Pause: pause}), nil
}
