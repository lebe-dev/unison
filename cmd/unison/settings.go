package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/settingsfile"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// settings prints the settings of the daemon and changes the lifetimes of the
// history.
func (a *app) settings(ctx context.Context, args []string) error {
	command, rest := subcommand(args)
	switch command {
	case "":
		flags := a.newFlagSet("settings")
		positional, err := parseCommand(flags, rest)
		if err != nil {
			return err
		}
		if len(positional) != 0 {
			return a.usageError("settings takes no arguments")
		}
		return a.showSettings(ctx)
	case "ttl":
		return a.settingsTTL(ctx, rest)
	case "remote":
		return a.settingsRemote(ctx, rest)
	case "export":
		return a.settingsExport(ctx, rest)
	case "import":
		return a.settingsImport(ctx, rest)
	default:
		return a.usageError("unknown settings command %q", command)
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
	a.field("ad params", onOff(settings.GetStripTracking()))
	a.field("remote changes", onOff(settings.GetAllowRemoteSettings()))
	return a.writeTTLs(settings)
}

// settingsRemote turns the permission of every paired device to change the
// confirmation flags of this device remotely on or off.
func (a *app) settingsRemote(ctx context.Context, args []string) error {
	flags := a.newFlagSet("settings remote")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return a.usageError("settings remote needs on or off")
	}
	allow, err := onOffValue(positional[0])
	if err != nil {
		return a.usageError("%v", err)
	}
	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		AllowRemoteSettings: &allow,
	}))
	if err != nil {
		return fmt.Errorf("change the remote settings permission: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.info("remote settings for every device are %s", onOff(allow))
	return nil
}

// settingsExport writes the settings as a JSON document, or prints it when no
// path is given.
func (a *app) settingsExport(ctx context.Context, args []string) error {
	flags := a.newFlagSet("settings export")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) > 1 {
		return a.usageError("settings export takes at most one path")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.ExportSettings(ctx, connect.NewRequest(&localv1.ExportSettingsRequest{}))
	if err != nil {
		return fmt.Errorf("export the settings: %w", err)
	}
	document := response.Msg.GetDocument()

	if len(positional) == 0 {
		if _, err := a.stdout.Write(document); err != nil {
			return fmt.Errorf("write the settings: %w", err)
		}
		return nil
	}
	path := positional[0]
	//nolint:gosec // The user names the file the settings are written to.
	if err := os.WriteFile(path, document, 0o600); err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	if a.json {
		return a.writeJSONValue(struct {
			Path     string `json:"path"`
			FileName string `json:"file_name"`
		}{Path: path, FileName: response.Msg.GetFileName()})
	}
	a.info("saved %s", path)
	return nil
}

// settingsImport applies the settings of a JSON document.
func (a *app) settingsImport(ctx context.Context, args []string) error {
	flags := a.newFlagSet("settings import")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return a.usageError("settings import takes one path")
	}

	document, err := readSettingsFile(positional[0], a.stdin)
	if err != nil {
		return err
	}
	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.ImportSettings(ctx, connect.NewRequest(&localv1.ImportSettingsRequest{
		Document: document,
	}))
	if err != nil {
		return fmt.Errorf("import the settings: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	if response.Msg.GetFromNewerVersion() {
		a.info("the file comes from a newer app, so only the known settings were applied")
	}
	if ignored := response.Msg.GetIgnored(); len(ignored) > 0 {
		a.info("skipped unknown keys: %s", strings.Join(ignored, ", "))
	}
	a.info("settings imported")
	return nil
}

// readSettingsFile returns the document of a path, or of standard input for
// "-".
func readSettingsFile(path string, stdin io.Reader) ([]byte, error) {
	reader := stdin
	if path != "-" {
		//nolint:gosec // The user names the file the settings are read from.
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read the settings: %w", err)
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	document, err := io.ReadAll(io.LimitReader(reader, settingsfile.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the settings: %w", err)
	}
	if len(document) > settingsfile.MaxBytes {
		return nil, fmt.Errorf("the settings file is larger than %d bytes", settingsfile.MaxBytes)
	}
	return document, nil
}

// settingsTTL prints or changes the lifetimes of the history.
func (a *app) settingsTTL(ctx context.Context, args []string) error {
	flags := a.newFlagSet("settings ttl")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	args = positional

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
