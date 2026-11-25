package main

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/history"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestSettings checks the human readable settings.
func TestSettings(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 140, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings")
	if code != exitOK {
		t.Fatalf("settings exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"device name", "blob limit", "downloads", "KIND", "LIFETIME"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("settings does not report %q:\n%s", want, stdout)
		}
	}
	if got := ttlOf(t, stdout, "text"); got != "1d" {
		t.Errorf("the text lifetime is %q, want 1d", got)
	}
	if got := ttlOf(t, stdout, "images"); got != "7d" {
		t.Errorf("the image lifetime is %q, want 7d", got)
	}
	if got := ttlOf(t, stdout, "files"); got != "7d" {
		t.Errorf("the file lifetime is %q, want 7d", got)
	}
}

// ttlOf returns the lifetime of a kind from the table of the command.
func ttlOf(t *testing.T, output, kind string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == kind {
			return fields[1]
		}
	}
	t.Fatalf("the output holds no %q lifetime:\n%s", kind, output)
	return ""
}

// TestSettingsTTL changes one lifetime, reads it back and resets it.
func TestSettingsTTL(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 150, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "ttl", "text", "30d")
	if code != exitOK {
		t.Fatalf("settings ttl exited with %d, stderr %q", code, stderr)
	}
	if got := ttlOf(t, stdout, "text"); got != "30d" {
		t.Errorf("the text lifetime is %q, want 30d", got)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "settings", "ttl")
	if code != exitOK {
		t.Fatalf("settings ttl exited with %d, stderr %q", code, stderr)
	}
	if got := ttlOf(t, stdout, "text"); got != "30d" {
		t.Errorf("the stored text lifetime is %q, want 30d", got)
	}
	if got := ttlOf(t, stdout, "images"); got != "7d" {
		t.Errorf("the stored image lifetime is %q, want 7d", got)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "settings", "ttl", "reset")
	if code != exitOK {
		t.Fatalf("settings ttl reset exited with %d, stderr %q", code, stderr)
	}
	if got := ttlOf(t, stdout, "text"); got != "1d" {
		t.Errorf("the text lifetime is %q after the reset, want 1d", got)
	}
}

// TestSettingsRemote checks the switch that covers every paired device.
func TestSettingsRemote(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 190, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "remote", "off")
	if code != exitOK {
		t.Fatalf("settings remote exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "off") {
		t.Errorf("the command does not report the permission:\n%s", stdout)
	}
	allowed, err := device.Trust().AllowRemoteSettings(t.Context())
	if err != nil {
		t.Fatalf("read the permission: %v", err)
	}
	if allowed {
		t.Fatal("the permission is still granted")
	}

	if code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "remote", "on"); code != exitOK {
		t.Fatalf("settings remote on exited with %d, stderr %q", code, stderr)
	}
	allowed, err = device.Trust().AllowRemoteSettings(t.Context())
	if err != nil {
		t.Fatalf("read the permission: %v", err)
	}
	if !allowed {
		t.Fatal("the permission is still denied")
	}
}

// TestSettingsTTLJSON checks the machine readable lifetimes.
func TestSettingsTTLJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 160, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "--json", "settings", "ttl")
	if code != exitOK {
		t.Fatalf("settings ttl exited with %d, stderr %q", code, stderr)
	}

	var response localv1.GetSettingsResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the settings %q: %v", stdout, err)
	}
	want := int64(history.DefaultTextTTL / time.Second)
	if got := response.GetSettings().GetHistoryTtlTextSeconds(); got != want {
		t.Errorf("text lifetime is %d, want %d", got, want)
	}
}

// TestSettingsTTLSetJSON checks the machine readable answer of a change.
func TestSettingsTTLSetJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 180, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "--json", "settings", "ttl", "text", "30d")
	if code != exitOK {
		t.Fatalf("settings ttl exited with %d, stderr %q", code, stderr)
	}

	var response localv1.UpdateSettingsResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the answer %q: %v", stdout, err)
	}
	want := int64(30 * 24 * 60 * 60)
	if got := response.GetSettings().GetHistoryTtlTextSeconds(); got != want {
		t.Errorf("text lifetime is %d, want %d", got, want)
	}
}

// TestSettingsTTLInvalid checks the refused lifetimes.
func TestSettingsTTLInvalid(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 170, api.Options{})
	device.start(t)

	cases := [][]string{
		{"settings", "ttl", "text", "0h"},
		{"settings", "ttl", "text", "tomorrow"},
		{"settings", "ttl", "photo", "1d"},
		{"settings", "ttl", "text"},
	}
	for _, args := range cases {
		command := append([]string{"--endpoint", device.endpoint}, args...)
		code, _, _ := runCommand(t, "", command...)
		if code != exitFailure {
			t.Errorf("%v exited with %d, want %d", args, code, exitFailure)
		}
	}

	// A lifetime above the limit is refused by the daemon.
	code, _, _ := runCommand(t, "", "--endpoint", device.endpoint, "settings", "ttl", "text", "400d")
	if code != exitFailure {
		t.Errorf("an excessive lifetime exited with %d, want %d", code, exitFailure)
	}
}

// TestParseLifetime checks the accepted lifetime forms.
func TestParseLifetime(t *testing.T) {
	cases := map[string]int64{
		"7d":  7 * 24 * 3600,
		"24h": 24 * 3600,
		"90m": 90 * 60,
		"45s": 45,
		"7D":  7 * 24 * 3600,
	}
	for value, want := range cases {
		got, err := parseLifetime(value)
		if err != nil {
			t.Errorf("%q: %v", value, err)
			continue
		}
		if got != want {
			t.Errorf("%q is %d seconds, want %d", value, got, want)
		}
	}
	for _, value := range []string{"", "0d", "-1d", "week", "0h", "500ms"} {
		if _, err := parseLifetime(value); err == nil {
			t.Errorf("%q is accepted", value)
		}
	}
}

// TestLifetimeText checks the rendered lifetimes.
func TestLifetimeText(t *testing.T) {
	cases := map[int64]string{
		7 * 24 * 3600: "7d",
		24 * 3600:     "1d",
		90 * 60:       "90m",
		45:            "45s",
		0:             "",
	}
	for seconds, want := range cases {
		if got := lifetimeText(seconds); got != want {
			t.Errorf("%d seconds is %q, want %q", seconds, got, want)
		}
	}
}
