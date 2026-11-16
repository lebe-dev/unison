package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// settings prints the settings of the daemon and changes the lifetimes of the
// history.
func (a *app) settings(ctx context.Context, args []string) error {
	flags := a.newFlagSet("settings")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	rest := flags.Args()
	switch {
	case len(rest) == 0:
		return a.showSettings(ctx)
	case rest[0] == "ttl":
		return a.settingsTTL(ctx, rest[1:])
	default:
		return a.usageError("unknown settings command %q", rest[0])
	}
}

// showSettings prints the settings of the daemon.
func (a *app) showSettings(ctx context.Context) error {
	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		return fmt.Errorf("read the settings: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}

	settings := response.Msg.GetSettings()
	a.field("device name", settings.GetDeviceName())
	a.field("blob limit", sizeText(uint64(settings.GetBlobMaxSizeBytes())))
	a.field("downloads", settings.GetDownloadsDir())
	return a.writeTTLs(settings)
}

// settingsTTL prints or changes the lifetimes of the history.
func (a *app) settingsTTL(ctx context.Context, args []string) error {
	client, err := a.api()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		response, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
		if err != nil {
			return fmt.Errorf("read the settings: %w", err)
		}
		if a.json {
			return a.writeJSON(response.Msg)
		}
		return a.writeTTLs(response.Msg.GetSettings())
	}
	switch {
	case args[0] == "reset" && len(args) == 1:
		response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
			ResetHistoryTtl: true,
		}))
		if err != nil {
			return fmt.Errorf("reset the history lifetimes: %w", err)
		}
		return a.writeUpdatedTTLs(response.Msg)
	case len(args) == 2:
		request, err := ttlRequest(args[0], args[1])
		if err != nil {
			return a.usageError("%v", err)
		}
		response, err := client.UpdateSettings(ctx, connect.NewRequest(request))
		if err != nil {
			return fmt.Errorf("change the history lifetime: %w", err)
		}
		return a.writeUpdatedTTLs(response.Msg)
	default:
		return a.usageError("settings ttl takes <text|image|files> <lifetime> or reset")
	}
}

// writeUpdatedTTLs prints the lifetimes of an update answer.
func (a *app) writeUpdatedTTLs(response *localv1.UpdateSettingsResponse) error {
	if a.json {
		return a.writeJSON(response)
	}
	return a.writeTTLs(response.GetSettings())
}

// writeTTLs prints one line per kind of the history.
func (a *app) writeTTLs(settings *localv1.Settings) error {
	rows := [][]string{{"KIND", "LIFETIME"}}
	for _, item := range []struct {
		name    string
		seconds int64
	}{
		{"text", settings.GetHistoryTtlTextSeconds()},
		{"images", settings.GetHistoryTtlImageSeconds()},
		{"files", settings.GetHistoryTtlFilesSeconds()},
	} {
		rows = append(rows, []string{item.name, lifetimeText(item.seconds)})
	}
	return a.writeRows(rows)
}

// ttlRequest builds the request that changes one lifetime.
func ttlRequest(kind, value string) (*localv1.UpdateSettingsRequest, error) {
	seconds, err := parseLifetime(value)
	if err != nil {
		return nil, err
	}
	request := &localv1.UpdateSettingsRequest{}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "text":
		request.HistoryTtlTextSeconds = &seconds
	case "image", "images":
		request.HistoryTtlImageSeconds = &seconds
	case "file", "files":
		request.HistoryTtlFilesSeconds = &seconds
	default:
		return nil, fmt.Errorf("unknown kind %q, want text, image or files", kind)
	}
	return request, nil
}

// parseLifetime parses a lifetime like "24h", "90m" or "7d" into seconds.
func parseLifetime(value string) (int64, error) {
	text := strings.ToLower(strings.TrimSpace(value))
	days, ok := strings.CutSuffix(text, "d")
	if !ok {
		lifetime, err := time.ParseDuration(text)
		if err != nil || lifetime < time.Second {
			return 0, fmt.Errorf("invalid lifetime %q, use a value like 24h or 7d", value)
		}
		return int64(lifetime.Round(time.Second) / time.Second), nil
	}
	count, err := strconv.ParseInt(days, 10, 64)
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("invalid lifetime %q, use a value like 24h or 7d", value)
	}
	return count * int64(24*time.Hour/time.Second), nil
}

// lifetimeText renders the seconds of a lifetime in whole days or hours.
func lifetimeText(seconds int64) string {
	lifetime := time.Duration(seconds) * time.Second
	switch {
	case lifetime <= 0:
		return ""
	case lifetime%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", lifetime/(24*time.Hour))
	case lifetime%time.Hour == 0:
		return fmt.Sprintf("%dh", lifetime/time.Hour)
	case lifetime%time.Minute == 0:
		return fmt.Sprintf("%dm", lifetime/time.Minute)
	default:
		return fmt.Sprintf("%ds", lifetime/time.Second)
	}
}
