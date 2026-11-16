package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

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
		if err := s.node.SetDeviceName(ctx, name); err != nil {
			return nil, errorOf(err)
		}
	}
	if value := req.Msg.RequireConfirmation; value != nil {
		if err := s.node.SetConfirmation(ctx, s.node.DeviceID(), trust.ScopeClipboard, *value); err != nil {
			return nil, errorOf(err)
		}
	}
	if value := req.Msg.RequireFileConfirmation; value != nil {
		if err := s.node.SetConfirmation(ctx, s.node.DeviceID(), trust.ScopeFiles, *value); err != nil {
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
	if value := req.Msg.DownloadsDir; value != nil {
		dir, err := downloadsDir(*value)
		if err != nil {
			return nil, err
		}
		if err := s.node.Store().Settings().SetString(ctx, share.SettingDownloadsDir, dir); err != nil {
			return nil, errorOf(err)
		}
	}
	if err := s.applyPolicy(ctx, req.Msg); err != nil {
		return nil, errorOf(err)
	}
	if err := s.applyHistoryTTLs(ctx, req.Msg); err != nil {
		return nil, errorOf(err)
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

	stored, err := s.deviceNameValue(ctx)
	if err != nil {
		return nil, err
	}

	self, err := s.node.Trust().SelfConfirmations(ctx)
	if err != nil {
		return nil, err
	}
	limit, err := s.node.Blobs().Limit(ctx)
	if err != nil {
		return nil, err
	}
	dir, err := s.node.Store().Settings().GetString(ctx, share.SettingDownloadsDir, "")
	if err != nil {
		return nil, err
	}
	if dir == "" {
		dir = share.DefaultDownloadsDir()
	}
	settings := &localv1.Settings{
		DeviceName:              stored,
		RequireConfirmation:     self.Clipboard,
		RequireFileConfirmation: self.Files,
		BlobMaxSizeBytes:        limit,
		DownloadsDir:            dir,
	}
	fillPolicy(settings, s.node.Policy().Config())
	fillHistoryTTLs(settings, s.node.TTLs())
	return settings, nil
}

// deviceNameValue returns the name this device announces: the stored name or
// the name the node started with.
func (s *Service) deviceNameValue(ctx context.Context) (string, error) {
	name, err := s.node.Store().Settings().GetString(ctx, node.SettingDeviceName, "")
	if err != nil {
		return "", err
	}
	if name == "" {
		return s.name, nil
	}
	return name, nil
}

// downloadsDir validates the directory of received files.
func downloadsDir(value string) (string, *connect.Error) {
	dir := strings.TrimSpace(value)
	switch {
	case dir == "":
		return share.DefaultDownloadsDir(), nil
	case !utf8.ValidString(dir):
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("downloads directory is not valid UTF-8"))
	case len(dir) > maxPathLength:
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("downloads directory is longer than %d bytes", maxPathLength))
	}
	return dir, nil
}
