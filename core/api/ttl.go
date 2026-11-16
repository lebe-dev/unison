package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// ttlRequested reports whether the request carries a history lifetime change.
func ttlRequested(req *localv1.UpdateSettingsRequest) bool {
	return req.GetResetHistoryTtl() ||
		req.HistoryTtlTextSeconds != nil ||
		req.HistoryTtlImageSeconds != nil ||
		req.HistoryTtlFilesSeconds != nil
}

// applyHistoryTTLs stores the lifetimes of the request and applies them to the
// running pipeline. A missing value keeps the stored one, and the items already
// recorded keep their expiry.
func (s *Service) applyHistoryTTLs(ctx context.Context, req *localv1.UpdateSettingsRequest) error {
	if !ttlRequested(req) {
		return nil
	}

	changes := []struct {
		seconds *int64
		key     string
	}{
		{req.HistoryTtlTextSeconds, history.SettingTextTTL},
		{req.HistoryTtlImageSeconds, history.SettingImageTTL},
		{req.HistoryTtlFilesSeconds, history.SettingFilesTTL},
	}
	for _, change := range changes {
		if change.seconds == nil {
			continue
		}
		if err := checkTTL(*change.seconds); err != nil {
			return err
		}
	}

	settings := s.node.Store().Settings()
	if req.GetResetHistoryTtl() {
		for _, change := range changes {
			if err := settings.Remove(ctx, change.key); err != nil {
				return err
			}
		}
	}
	for _, change := range changes {
		if change.seconds == nil {
			continue
		}
		lifetime := time.Duration(*change.seconds) * time.Second
		if err := settings.SetDuration(ctx, change.key, lifetime); err != nil {
			return err
		}
	}
	return s.node.ReloadTTLs(ctx)
}

// checkTTL reports a lifetime outside the accepted range.
func checkTTL(seconds int64) *connect.Error {
	switch {
	case seconds <= 0:
		return connect.NewError(connect.CodeInvalidArgument, errors.New("history lifetime must be positive"))
	case seconds > int64(history.MaxTTL/time.Second):
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("history lifetime is longer than %d days", int(history.MaxTTL/(24*time.Hour))))
	}
	return nil
}

// fillHistoryTTLs reports the lifetimes of the pipeline in settings.
func fillHistoryTTLs(settings *localv1.Settings, ttls clip.TTLs) {
	settings.HistoryTtlTextSeconds = int64(ttls.Text.Round(time.Second) / time.Second)
	settings.HistoryTtlImageSeconds = int64(ttls.Image.Round(time.Second) / time.Second)
	settings.HistoryTtlFilesSeconds = int64(ttls.Files.Round(time.Second) / time.Second)
}
