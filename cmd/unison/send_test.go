package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/api"
)

// sendDevices starts two paired devices and waits until they see each other.
func sendDevices(t *testing.T, base int) (*testDevice, *testDevice) {
	t.Helper()

	network, bus := newNetwork(t)
	a := newDevice(t, network, bus, base, api.Options{})
	b := newDevice(t, network, bus, base+1000, api.Options{})
	a.start(t)
	b.start(t)
	a.pair(t, b)
	b.pair(t, a)

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if len(a.Share().Peers()) == 1 && len(b.Share().Peers()) == 1 {
			return a, b
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the devices did not connect")
	return nil, nil
}

// waitSaved waits until the downloads directory of the device holds one file
// with the content.
func waitSaved(t *testing.T, device *testDevice, want []byte) string {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(device.downloads)
		if err == nil {
			for _, entry := range entries {
				path := filepath.Join(device.downloads, entry.Name())
				data, err := os.ReadFile(path)
				if err == nil && string(data) == string(want) {
					return path
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no saved file with %d bytes", len(want))
	return ""
}

func TestSendFile(t *testing.T) {
	a, b := sendDevices(t, 30000)

	data := []byte(strings.Repeat("unison ", 4096))
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	code, stdout, stderr := runCommand(t, "", "send", path, "--to", b.name, "--endpoint", a.endpoint)
	if code != exitOK {
		t.Fatalf("send exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "offered") || !strings.Contains(stdout, "note.txt") {
		t.Fatalf("the output is %q", stdout)
	}
	if saved := waitSaved(t, b, data); filepath.Base(saved) != "note.txt" {
		t.Fatalf("the file was saved as %q", saved)
	}
}

func TestSendStdin(t *testing.T) {
	a, b := sendDevices(t, 32000)

	data := []byte(strings.Repeat("piped content ", 2048))
	code, stdout, stderr := runCommand(t, string(data), "send", "-", "--name", "piped.txt",
		"--to", b.name, "--endpoint", a.endpoint)
	if code != exitOK {
		t.Fatalf("send exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if saved := waitSaved(t, b, data); filepath.Base(saved) != "piped.txt" {
		t.Fatalf("the file was saved as %q", saved)
	}
}

func TestSendNeedsReceiver(t *testing.T) {
	a, _ := sendDevices(t, 34000)

	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	code, _, stderr := runCommand(t, "", "send", path, "--endpoint", a.endpoint)
	if code != exitFailure {
		t.Fatalf("send exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "--to") {
		t.Fatalf("the output is %q", stderr)
	}

	code, _, stderr = runCommand(t, "", "send", path, "--to", "unknown-device", "--endpoint", a.endpoint)
	if code != exitFailure {
		t.Fatalf("send to an unknown device exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "no device matches") {
		t.Fatalf("the output is %q", stderr)
	}
}
