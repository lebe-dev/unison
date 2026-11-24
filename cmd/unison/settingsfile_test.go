package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/settingsfile"
)

// TestSettingsExportToFile checks that the command writes a document and the
// import restores it.
func TestSettingsExportToFile(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 1600, api.Options{})
	device.start(t)

	if code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "ttl", "text", "30d"); code != exitOK {
		t.Fatalf("settings ttl exited with %d, stderr %q", code, stderr)
	}

	path := filepath.Join(t.TempDir(), "unison-settings.json")
	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "export", path)
	if code != exitOK {
		t.Fatalf("settings export exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, path) {
		t.Errorf("the command does not report the path:\n%s", stdout)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file: %v", err)
	}
	doc, result, err := settingsfile.Decode(data)
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	if result.FromNewer || len(result.Unknown) != 0 {
		t.Errorf("the document reports %+v", result)
	}
	if *doc.Core.HistoryTTL.Text != 30*86400 {
		t.Errorf("the text lifetime is %d", *doc.Core.HistoryTTL.Text)
	}

	if code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "ttl", "text", "1d"); code != exitOK {
		t.Fatalf("settings ttl exited with %d, stderr %q", code, stderr)
	}
	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "settings", "import", path)
	if code != exitOK {
		t.Fatalf("settings import exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "settings imported") {
		t.Errorf("the command does not report the import:\n%s", stdout)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "settings", "ttl")
	if code != exitOK {
		t.Fatalf("settings ttl exited with %d, stderr %q", code, stderr)
	}
	if got := ttlOf(t, stdout, "text"); got != "30d" {
		t.Errorf("the restored text lifetime is %q, want 30d", got)
	}
}

// TestSettingsExportPrints checks that a command without a path prints the
// document.
func TestSettingsExportPrints(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 1610, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "export")
	if code != exitOK {
		t.Fatalf("settings export exited with %d, stderr %q", code, stderr)
	}
	if _, _, err := settingsfile.Decode([]byte(stdout)); err != nil {
		t.Errorf("the printed document is not readable: %v", err)
	}
}

// TestSettingsImportStdin checks that a document reaches the daemon through
// standard input.
func TestSettingsImportStdin(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 1620, api.Options{})
	device.start(t)

	document := `{"schema": "unison-settings", "format_version": 1, "core": {"strip_tracking": false}}`
	code, stdout, stderr := runCommand(t, document, "--endpoint", device.endpoint, "settings", "import", "-")
	if code != exitOK {
		t.Fatalf("settings import exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "settings imported") {
		t.Errorf("the command does not report the import:\n%s", stdout)
	}
}

// TestSettingsImportNewerWarns checks the warning of a document of a newer app
// and the keys this version skipped.
func TestSettingsImportNewerWarns(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 1630, api.Options{})
	device.start(t)

	document := `{"schema": "unison-settings", "format_version": 99, "core": {"strip_tracking": false}}`
	code, stdout, stderr := runCommand(t, document, "--endpoint", device.endpoint, "settings", "import", "-")
	if code != exitOK {
		t.Fatalf("settings import exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "newer app") {
		t.Errorf("the command does not report the newer version:\n%s", stdout)
	}

	document = `{"schema": "unison-settings", "format_version": 1, "core": {"future": 1}}`
	code, stdout, stderr = runCommand(t, document, "--endpoint", device.endpoint, "settings", "import", "-")
	if code != exitOK {
		t.Fatalf("settings import exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "core.future") {
		t.Errorf("the command does not report the skipped key:\n%s", stdout)
	}
}

// TestSettingsImportBrokenFile checks the error of a file that is not a
// settings document.
func TestSettingsImportBrokenFile(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 1640, api.Options{})
	device.start(t)

	code, _, stderr := runCommand(t, `{"schema": "other"}`, "--endpoint", device.endpoint, "settings", "import", "-")
	if code != exitFailure {
		t.Fatalf("settings import exited with %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "settings file") {
		t.Errorf("the error does not explain the file:\n%s", stderr)
	}
}

// TestSettingsImportMissingFile checks the error of a path that does not exist.
func TestSettingsImportMissingFile(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 1650, api.Options{})
	device.start(t)

	path := filepath.Join(t.TempDir(), "missing.json")
	code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "settings", "import", path)
	if code != exitFailure {
		t.Fatalf("settings import exited with %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "read the settings") {
		t.Errorf("the error does not explain the file:\n%s", stderr)
	}
}
