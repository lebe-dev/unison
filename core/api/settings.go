package api

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/blob"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// settingDeviceName is the settings key of the device name used at the next
// start.
const settingDeviceName = "device.name"

// invalidSizeError reports a store size limit that is not positive.
func invalidSizeError() *connect.Error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New("blob size limit must be positive"))
}

// GetSettings returns the settings of the daemon.
func (s *Service) GetSettings(ctx context.Context, _ *connect.Request[localv1.GetSettingsRequest]) (*connect.Response[localv1.GetSettingsResponse], error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.GetSettingsResponse{Settings: settings}), nil
}

// UpdateSettings stores the settings given in the request and returns the
// resulting settings.
func (s *Service) UpdateSettings(ctx context.Context, req *connect.Request[localv1.UpdateSettingsRequest]) (*connect.Response[localv1.UpdateSettingsResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	if value := req.Msg.DeviceName; value != nil {
		name, err := deviceName(*value)
		if err != nil {
			return nil, err
		}
		if err := s.node.Store().Settings().SetString(ctx, settingDeviceName, name); err != nil {
			return nil, errorOf(err)
		}
	}
	if value := req.Msg.RequireConfirmation; value != nil {
		if err := s.node.Trust().SetSelfRequireConfirmation(ctx, *value); err != nil {
			return nil, errorOf(err)
		}
	}
	if value := req.Msg.BlobMaxSizeBytes; value != nil {
		if *value <= 0 {
			return nil, invalidSizeError()
		}
		if err := s.node.Store().Settings().SetInt(ctx, blob.SettingMaxSize, *value); err != nil {
			return nil, errorOf(err)
		}
	}

	settings, err := s.settings(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.UpdateSettingsResponse{Settings: settings}), nil
}

// settings reads the stored settings.
func (s *Service) settings(ctx context.Context) (*localv1.Settings, error) {
	if err := s.requireRunning(); err != nil {
		return nil, err
	}

	stored, err := s.node.Store().Settings().GetString(ctx, settingDeviceName, "")
	if err != nil {
		return nil, err
	}
	if stored == "" {
		stored = s.name
	}

	require, err := s.node.Trust().SelfRequireConfirmation(ctx)
	if err != nil {
		return nil, err
	}
	limit, err := s.node.Blobs().Limit(ctx)
	if err != nil {
		return nil, err
	}
	return &localv1.Settings{
		DeviceName:          stored,
		RequireConfirmation: require,
		BlobMaxSizeBytes:    limit,
	}, nil
}
