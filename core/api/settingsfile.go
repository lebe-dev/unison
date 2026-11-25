package api

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/settingsfile"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// ExportSettings writes the settings of the daemon as a JSON document.
func (s *Service) ExportSettings(ctx context.Context, req *connect.Request[localv1.ExportSettingsRequest]) (*connect.Response[localv1.ExportSettingsResponse], error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	app, invalid := appSection(req.Msg.GetAppSettings())
	if invalid != nil {
		return nil, invalid
	}

	now := s.clock.Now().UTC()
	platform := settingsfile.PlatformName(runtime.GOOS)
	document, err := settingsfile.Encode(settingsfile.Document{
		ExportedAt: now.Format(time.RFC3339),
		Platform:   platform,
		AppVersion: s.version,
		Core:       coreOfSettings(settings),
		App:        app,
	})
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ExportSettingsResponse{
		Document: document,
		FileName: settingsfile.FileName(platform, now),
	}), nil
}

// ImportSettings applies the settings of a JSON document. Keys that the
// document does not carry keep their stored values.
func (s *Service) ImportSettings(ctx context.Context, req *connect.Request[localv1.ImportSettingsRequest]) (*connect.Response[localv1.ImportSettingsResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}
	doc, result, err := settingsfile.Decode(req.Msg.GetDocument())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if doc.Core != nil {
		ignored := result.Unknown
		// A daemon without the application log cannot take its setting, so the
		// value is reported instead of failing the whole import.
		if doc.Core.JournalEnabled != nil && s.journal == nil {
			doc.Core.JournalEnabled = nil
			ignored = append(ignored, "core.journal_enabled")
			sort.Strings(ignored)
		}
		update, invalid := updateRequestOf(doc.Core)
		if invalid != nil {
			return nil, invalid
		}
		if _, err := s.UpdateSettings(ctx, connect.NewRequest(update)); err != nil {
			return nil, err
		}
		result.Unknown = ignored
	}

	settings, err := s.settings(ctx)
	if err != nil {
		return nil, errorOf(err)
	}
	return connect.NewResponse(&localv1.ImportSettingsResponse{
		Settings:         settings,
		Ignored:          result.Unknown,
		FromNewerVersion: result.FromNewer,
	}), nil
}

// appSection reads the local settings of the app. An empty value stands for an
// app without local settings.
func appSection(value string) (map[string]json.RawMessage, *connect.Error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	var section map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &section); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the local settings of the app are not a JSON object"))
	}
	if section == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the local settings of the app are not a JSON object"))
	}
	return section, nil
}

// coreOfSettings describes the settings of the daemon in the document format.
func coreOfSettings(settings *localv1.Settings) *settingsfile.Core {
	return &settingsfile.Core{
		RequireConfirmation:     protoPtr(settings.GetRequireConfirmation()),
		RequireFileConfirmation: protoPtr(settings.GetRequireFileConfirmation()),
		AllowRemoteSettings:     protoPtr(settings.GetAllowRemoteSettings()),
		BlobMaxSizeBytes:        protoPtr(settings.GetBlobMaxSizeBytes()),
		StripTracking:           protoPtr(settings.GetStripTracking()),
		SentryEnabled:           protoPtr(settings.GetSentryEnabled()),
		JournalEnabled:          protoPtr(settings.GetJournalEnabled()),
		HistoryTTL: &settingsfile.HistoryTTL{
			Text:  protoPtr(settings.GetHistoryTtlTextSeconds()),
			Image: protoPtr(settings.GetHistoryTtlImageSeconds()),
			Files: protoPtr(settings.GetHistoryTtlFilesSeconds()),
		},
		Policy: &settingsfile.Policy{
			RelayMaxSizeBytes: protoPtr(settings.GetRelayMaxSizeBytes()),
			Unmetered:         policyRowOf(settings.GetUnmetered()),
			Metered:           policyRowOf(settings.GetMetered()),
			Roaming:           policyRowOf(settings.GetRoaming()),
			DataSaver:         policyRowOf(settings.GetDataSaver()),
		},
	}
}

// policyRowOf describes one row of the policy in the document format.
func policyRowOf(row *localv1.PolicyRow) *settingsfile.PolicyRow {
	if row == nil {
		return nil
	}
	return &settingsfile.PolicyRow{
		Text:  policyCellOfProto(row.GetText()),
		Image: policyCellOfProto(row.GetImage()),
		Files: policyCellOfProto(row.GetFiles()),
		Relay: row.Relay,
	}
}

// policyCellOfProto describes one cell of the policy in the document format.
func policyCellOfProto(cell *localv1.PolicyCell) *settingsfile.PolicyCell {
	if cell == nil {
		return nil
	}
	described := &settingsfile.PolicyCell{MaxSizeBytes: cell.MaxSizeBytes}
	if cell.Action != nil {
		described.Action = protoPtr(policyActionText(*cell.Action))
	}
	return described
}

// policyActionText returns the document name of a policy action.
func policyActionText(action localv1.PolicyAction) string {
	parsed, err := policyActionOf(action)
	if err != nil {
		return policy.ActionReject.String()
	}
	return parsed.String()
}

// updateRequestOf describes the document settings as a request of the daemon.
func updateRequestOf(core *settingsfile.Core) (*localv1.UpdateSettingsRequest, *connect.Error) {
	req := &localv1.UpdateSettingsRequest{
		RequireConfirmation:     core.RequireConfirmation,
		RequireFileConfirmation: core.RequireFileConfirmation,
		AllowRemoteSettings:     core.AllowRemoteSettings,
		BlobMaxSizeBytes:        core.BlobMaxSizeBytes,
		StripTracking:           core.StripTracking,
		SentryEnabled:           core.SentryEnabled,
		JournalEnabled:          core.JournalEnabled,
	}
	if ttl := core.HistoryTTL; ttl != nil {
		req.HistoryTtlTextSeconds = ttl.Text
		req.HistoryTtlImageSeconds = ttl.Image
		req.HistoryTtlFilesSeconds = ttl.Files
	}
	if network := core.Policy; network != nil {
		req.RelayMaxSizeBytes = network.RelayMaxSizeBytes
		rows := []struct {
			row    *settingsfile.PolicyRow
			target **localv1.PolicyRow
		}{
			{network.Unmetered, &req.Unmetered},
			{network.Metered, &req.Metered},
			{network.Roaming, &req.Roaming},
			{network.DataSaver, &req.DataSaver},
		}
		for _, item := range rows {
			row, invalid := policyRowRequest(item.row)
			if invalid != nil {
				return nil, invalid
			}
			*item.target = row
		}
	}
	return req, nil
}

// policyRowRequest describes one document row as a request row.
func policyRowRequest(row *settingsfile.PolicyRow) (*localv1.PolicyRow, *connect.Error) {
	if row == nil {
		return nil, nil
	}
	request := &localv1.PolicyRow{Relay: row.Relay}
	rows := []struct {
		cell   *settingsfile.PolicyCell
		target **localv1.PolicyCell
	}{
		{row.Text, &request.Text},
		{row.Image, &request.Image},
		{row.Files, &request.Files},
	}
	for _, item := range rows {
		cell, invalid := policyCellRequest(item.cell)
		if invalid != nil {
			return nil, invalid
		}
		*item.target = cell
	}
	return request, nil
}

// policyCellRequest describes one document cell as a request cell.
func policyCellRequest(cell *settingsfile.PolicyCell) (*localv1.PolicyCell, *connect.Error) {
	if cell == nil {
		return nil, nil
	}
	request := &localv1.PolicyCell{MaxSizeBytes: cell.MaxSizeBytes}
	if cell.Action == nil {
		return request, nil
	}
	action, err := policy.ParseAction(*cell.Action)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	request.Action = protoPtr(policyActionProto(action))
	return request, nil
}
