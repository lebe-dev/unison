package main

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// labelWidth aligns the labels of the field blocks.
const labelWidth = 11

// field prints one label and its value of a field block.
func (a *app) field(label, value string) {
	_, _ = fmt.Fprintf(a.stdout, "%-*s %s\n", labelWidth, label, value)
}

// info prints a line of progress. With --json the line goes to stderr, so that
// stdout carries only JSON.
func (a *app) info(format string, args ...any) {
	w := a.stdout
	if a.json {
		w = a.stderr
	}
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}

// writeJSON writes a message as JSON. Fields that are unset keep their default
// value, so scripts see the same keys for every response.
func (a *app) writeJSON(message proto.Message) error {
	data, err := protojson.MarshalOptions{EmitDefaultValues: true, Indent: "  "}.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode the answer: %w", err)
	}
	_, err = fmt.Fprintf(a.stdout, "%s\n", data)
	return err
}

// writeEventJSON writes one event of the stream as a line of JSON.
func (a *app) writeEventJSON(event *localv1.Event) error {
	data, err := protojson.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode the event: %w", err)
	}
	_, err = fmt.Fprintf(a.stdout, "%s\n", data)
	return err
}

// writeTable writes the devices as a table.
func (a *app) writeTable(devices []*localv1.Device) error {
	rows := make([][]string, 0, len(devices)+1)
	rows = append(rows, []string{"FINGERPRINT", "NAME", "ROLE", "PLATFORM", "STATE", "CONFIRM", "PEER ASKS"})
	for _, device := range devices {
		rows = append(rows, []string{
			fingerprintOf(device.GetDeviceId()),
			device.GetName(),
			roleText(device.GetRole()),
			platformText(device.GetPlatform()),
			deviceState(device),
			onOff(device.GetRequireConfirmation()),
			yesNo(device.GetPeerRequireConfirmation()),
		})
	}
	return a.writeRows(rows)
}

// writeRows writes rows as a table with the columns aligned.
func (a *app) writeRows(rows [][]string) error {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], len(cell))
		}
	}
	for _, row := range rows {
		line := make([]byte, 0, 80)
		for i, cell := range row {
			if i > 0 {
				line = append(line, ' ', ' ')
			}
			line = append(line, cell...)
			if i < len(row)-1 {
				line = append(line, strings.Repeat(" ", widths[i]-len(cell))...)
			}
		}
		if _, err := fmt.Fprintf(a.stdout, "%s\n", line); err != nil {
			return err
		}
	}
	return nil
}

// fingerprintOf returns the short readable form of a device ID.
func fingerprintOf(id string) string {
	parsed, err := model.ParseDeviceID(id)
	if err != nil {
		return id
	}
	if parsed.IsZero() {
		return ""
	}
	return string(model.FingerprintOf(parsed))
}

// roleText returns the name of a role.
func roleText(role localv1.Role) string {
	switch role {
	case localv1.Role_ROLE_PERSONAL:
		return "personal"
	case localv1.Role_ROLE_FRIENDS:
		return "friends"
	default:
		return "unknown"
	}
}

// platformText returns the name of a platform.
func platformText(platform localv1.Platform) string {
	switch platform {
	case localv1.Platform_PLATFORM_MACOS:
		return "macos"
	case localv1.Platform_PLATFORM_WINDOWS:
		return "windows"
	case localv1.Platform_PLATFORM_ANDROID:
		return "android"
	default:
		return "unknown"
	}
}

// pathText returns the name of a network path.
func pathText(path localv1.Path) string {
	switch path {
	case localv1.Path_PATH_LAN:
		return "lan"
	case localv1.Path_PATH_RELAY:
		return "relay"
	default:
		return ""
	}
}

// deviceState returns the connection state of a device.
func deviceState(device *localv1.Device) string {
	if device.GetRevoked() {
		return "revoked"
	}
	if !device.GetOnline() {
		return "offline"
	}
	if path := pathText(device.GetPath()); path != "" {
		return "online " + path
	}
	return "online"
}

// onOff renders a flag of the device table.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// yesNo renders a flag of a peer in the device table.
func yesNo(on bool) string {
	if on {
		return "yes"
	}
	return "no"
}

// networkText describes the network conditions of the local device.
func networkText(state *localv1.NetworkState) string {
	if !state.GetOnline() {
		return "offline"
	}
	parts := []string{"online"}
	switch {
	case state.GetWifi():
		parts = append(parts, "wifi")
	case state.GetCellular():
		parts = append(parts, "cellular")
	}
	if state.GetMetered() {
		parts = append(parts, "metered")
	} else {
		parts = append(parts, "unmetered")
	}
	if state.GetRoaming() {
		parts = append(parts, "roaming")
	}
	if state.GetVpn() {
		parts = append(parts, "vpn")
	}
	if state.GetDataSaver() {
		parts = append(parts, "data saver")
	}
	return strings.Join(parts, ", ")
}

// countText returns a count with its noun in the right number.
func countText(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// pauseText describes the state of the clipboard pause.
func pauseText(pause *localv1.Pause) string {
	if !pause.GetPaused() {
		return "on"
	}
	if pause.GetIndefinite() {
		return "paused until turned off"
	}
	return "paused until " + formatTime(pause.GetUntilUnixMs())
}

// formatTime renders a time in unix milliseconds in the local zone. A time on
// the current day is shown without the date.
func formatTime(unixMs int64) string {
	t := time.UnixMilli(unixMs)
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05")
	}
	return t.Format("2006-01-02 15:04")
}
