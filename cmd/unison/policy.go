package main

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// policy prints the network policy of the daemon.
func (a *app) policy(ctx context.Context, args []string) error {
	flags := a.newFlagSet("policy")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("policy takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		return fmt.Errorf("read the policy: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}

	settings := response.Msg.GetSettings()
	rows := [][]string{{"CONDITION", "TEXT", "IMAGE", "FILES", "RELAY"}}
	for _, item := range []struct {
		name string
		row  *localv1.PolicyRow
	}{
		{"unmetered", settings.GetUnmetered()},
		{"metered", settings.GetMetered()},
		{"roaming", settings.GetRoaming()},
		{"data saver", settings.GetDataSaver()},
	} {
		rows = append(rows, []string{
			item.name,
			cellText(item.row.GetText()),
			cellText(item.row.GetImage()),
			cellText(item.row.GetFiles()),
			boolText(item.row.GetRelay()),
		})
	}
	if err := a.writeRows(rows); err != nil {
		return err
	}
	a.field("relay limit", sizeText(uint64(settings.GetRelayMaxSizeBytes())))
	return nil
}

// cellText describes one policy cell.
func cellText(cell *localv1.PolicyCell) string {
	action := actionText(cell.GetAction())
	if cell.GetAction() != localv1.PolicyAction_POLICY_ACTION_AUTO {
		return action
	}
	return action + " " + sizeText(uint64(cell.GetMaxSizeBytes()))
}

// actionText returns the name of a policy action.
func actionText(action localv1.PolicyAction) string {
	switch action {
	case localv1.PolicyAction_POLICY_ACTION_REJECT:
		return "reject"
	case localv1.PolicyAction_POLICY_ACTION_METADATA:
		return "metadata"
	case localv1.PolicyAction_POLICY_ACTION_CONFIRM:
		return "confirm"
	case localv1.PolicyAction_POLICY_ACTION_AUTO:
		return "auto"
	default:
		return "unknown"
	}
}

// boolText returns yes or no.
func boolText(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
