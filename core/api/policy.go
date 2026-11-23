package api

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// policyRequested reports whether the request carries a policy change.
func policyRequested(req *localv1.UpdateSettingsRequest) bool {
	return req.GetResetPolicy() ||
		req.RelayMaxSizeBytes != nil ||
		req.GetUnmetered() != nil ||
		req.GetMetered() != nil ||
		req.GetRoaming() != nil ||
		req.GetDataSaver() != nil
}

// applyPolicy stores the policy rows of the request and applies the resulting
// table to the running core. Rows and fields that are absent keep their value,
// so the defaults are never overwritten by a partial change.
func (s *Service) applyPolicy(ctx context.Context, req *localv1.UpdateSettingsRequest) error {
	if !policyRequested(req) {
		return nil
	}
	settings := s.node.Store().Settings()

	cfg := s.node.Policy().Config()
	if req.GetResetPolicy() {
		cfg = policy.Default()
	}

	rows := []struct {
		condition policy.Condition
		row       *localv1.PolicyRow
	}{
		{policy.ConditionUnmetered, req.GetUnmetered()},
		{policy.ConditionMetered, req.GetMetered()},
		{policy.ConditionRoaming, req.GetRoaming()},
		{policy.ConditionDataSaver, req.GetDataSaver()},
	}
	for _, item := range rows {
		if item.row == nil {
			continue
		}
		row, _ := cfg.Row(item.condition)
		if item.row.Relay != nil {
			row.Relay = *item.row.Relay
		}
		for _, kind := range policy.Kinds() {
			cell, _ := row.Cell(kind)
			patch := policyCellOf(item.row, kind)
			if patch == nil {
				continue
			}
			if patch.Action != nil {
				action, err := policyActionOf(*patch.Action)
				if err != nil {
					return err
				}
				cell.Action = action
			}
			if patch.MaxSizeBytes != nil {
				cell.MaxSize = *patch.MaxSizeBytes
			}
			row.SetCell(kind, cell)
		}
		cfg.SetRow(item.condition, row)
	}
	if req.RelayMaxSizeBytes != nil {
		cfg.RelayMaxSize = *req.RelayMaxSizeBytes
	}
	if err := cfg.Validate(); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	if req.GetResetPolicy() {
		for _, key := range policy.Keys() {
			if err := settings.Remove(ctx, key); err != nil {
				return err
			}
		}
	}
	for _, item := range rows {
		if item.row == nil {
			continue
		}
		if item.row.Relay != nil {
			if err := settings.SetBool(ctx, policy.RelayKey(item.condition), *item.row.Relay); err != nil {
				return err
			}
		}
		for _, kind := range policy.Kinds() {
			patch := policyCellOf(item.row, kind)
			if patch == nil {
				continue
			}
			if patch.Action != nil {
				action, err := policyActionOf(*patch.Action)
				if err != nil {
					return err
				}
				if err := settings.SetString(ctx, policy.ModeKey(item.condition, kind), action.String()); err != nil {
					return err
				}
			}
			if patch.MaxSizeBytes != nil {
				if err := settings.SetInt(ctx, policy.MaxSizeKey(item.condition, kind), *patch.MaxSizeBytes); err != nil {
					return err
				}
			}
		}
	}
	if req.RelayMaxSizeBytes != nil {
		if err := settings.SetInt(ctx, policy.RelayMaxSizeKey, *req.RelayMaxSizeBytes); err != nil {
			return err
		}
	}
	return s.node.ReloadPolicy(ctx)
}

// fillPolicy reports the policy table of the daemon in settings.
func fillPolicy(settings *localv1.Settings, cfg policy.Config) {
	settings.Unmetered = policyRowProto(cfg.Unmetered)
	settings.Metered = policyRowProto(cfg.Metered)
	settings.Roaming = policyRowProto(cfg.Roaming)
	settings.DataSaver = policyRowProto(cfg.DataSaver)
	settings.RelayMaxSizeBytes = cfg.RelayMaxSize
}

// policyRowProto returns the message of one row.
func policyRowProto(row policy.Row) *localv1.PolicyRow {
	return &localv1.PolicyRow{
		Text:  policyCellProto(row.Text),
		Image: policyCellProto(row.Image),
		Files: policyCellProto(row.Files),
		Relay: protoPtr(row.Relay),
	}
}

// policyCellProto returns the message of one cell.
func policyCellProto(cell policy.Cell) *localv1.PolicyCell {
	action := policyActionProto(cell.Action)
	size := cell.MaxSize
	return &localv1.PolicyCell{Action: &action, MaxSizeBytes: &size}
}

// policyCellOf returns the cell of kind in a request row, nil when the row does
// not mention it.
func policyCellOf(row *localv1.PolicyRow, kind model.Kind) *localv1.PolicyCell {
	switch kind {
	case model.KindText:
		return row.GetText()
	case model.KindImage:
		return row.GetImage()
	case model.KindFiles:
		return row.GetFiles()
	default:
		return nil
	}
}

// policyActionProto returns the message action of an action.
func policyActionProto(action policy.Action) localv1.PolicyAction {
	switch action {
	case policy.ActionMetadataOnly:
		return localv1.PolicyAction_POLICY_ACTION_METADATA
	case policy.ActionConfirm:
		return localv1.PolicyAction_POLICY_ACTION_CONFIRM
	case policy.ActionAutoApply:
		return localv1.PolicyAction_POLICY_ACTION_AUTO
	default:
		return localv1.PolicyAction_POLICY_ACTION_REJECT
	}
}

// policyActionOf returns the action of a message action.
func policyActionOf(value localv1.PolicyAction) (policy.Action, error) {
	switch value {
	case localv1.PolicyAction_POLICY_ACTION_REJECT:
		return policy.ActionReject, nil
	case localv1.PolicyAction_POLICY_ACTION_METADATA:
		return policy.ActionMetadataOnly, nil
	case localv1.PolicyAction_POLICY_ACTION_CONFIRM:
		return policy.ActionConfirm, nil
	case localv1.PolicyAction_POLICY_ACTION_AUTO:
		return policy.ActionAutoApply, nil
	default:
		return policy.ActionReject, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("unknown policy action %d", value))
	}
}

// protoPtr returns a pointer to v.
func protoPtr[T any](v T) *T {
	return &v
}
