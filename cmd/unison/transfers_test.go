package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/trust"
)

// waitTransferState waits until the table of the transfers reports the state of
// one transfer.
func waitTransferState(t *testing.T, d *testDevice, id, state string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		code, stdout, stderr := runCommand(t, "", "transfers", "--endpoint", d.endpoint)
		if code != exitOK {
			t.Fatalf("transfers exited with %d, stderr %q", code, stderr)
		}
		for _, line := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(line, id) && strings.Contains(line, state) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the transfer %s did not reach %q within %v", id, state, testTimeout)
}

// transferID returns the id of the only transfer of the device with the file.
func transferID(t *testing.T, d *testDevice, name string) string {
	t.Helper()

	code, stdout, stderr := runCommand(t, "", "transfers", "--endpoint", d.endpoint)
	if code != exitOK {
		t.Fatalf("transfers exited with %d, stderr %q", code, stderr)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.Contains(line, name) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] != "ID" {
			return fields[0]
		}
	}
	t.Fatalf("no transfer with %q", name)
	return ""
}

func TestTransfersList(t *testing.T) {
	a, b := friendDevices(t, 40000)
	path := filepath.Join(t.TempDir(), "listed.bin")
	if err := os.WriteFile(path, []byte("listed content"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	sendTo(t, a, b, path)
	waitRequestID(t, b, "listed.bin")

	code, stdout, stderr := runCommand(t, "", "transfers", "--endpoint", a.endpoint)
	if code != exitOK {
		t.Fatalf("transfers exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"ID", "DIRECTION", "out", b.name, "offered", "listed.bin"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the output has no %q:\n%s", want, stdout)
		}
	}

	code, stdout, stderr = runCommand(t, "", "transfers", "--endpoint", b.endpoint)
	if code != exitOK {
		t.Fatalf("transfers exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "in") || !strings.Contains(stdout, "pending") {
		t.Errorf("the receiver does not list the pending offer:\n%s", stdout)
	}
}

func TestTransfersDecline(t *testing.T) {
	a, b := friendDevices(t, 41000)
	path := filepath.Join(t.TempDir(), "refused.bin")
	if err := os.WriteFile(path, []byte("refused content"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	sendTo(t, a, b, path)

	id := waitRequestID(t, b, "refused.bin")
	if code, stdout, stderr := runCommand(t, "", "requests", "decline", id, "--endpoint", b.endpoint); code != exitOK {
		t.Fatalf("requests decline exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	waitTransferState(t, a, id, "declined")
}

func TestTransfersCancel(t *testing.T) {
	a, b := friendDevices(t, 42000)
	path := filepath.Join(t.TempDir(), "canceled.bin")
	if err := os.WriteFile(path, []byte("canceled content"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	sendTo(t, a, b, path)
	waitRequestID(t, b, "canceled.bin")

	id := transferID(t, a, "canceled.bin")
	code, stdout, stderr := runCommand(t, "", "transfers", "cancel", id, "--endpoint", a.endpoint)
	if code != exitOK {
		t.Fatalf("transfers cancel exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "canceled the transfer") {
		t.Fatalf("the output is %q", stdout)
	}
	waitTransferState(t, a, id, "canceled")
}

func TestTransfersUnknownID(t *testing.T) {
	a, _ := friendDevices(t, 43000)

	code, _, stderr := runCommand(t, "", "transfers", "cancel", "0000", "--endpoint", a.endpoint)
	if code != exitFailure {
		t.Fatalf("transfers cancel exited with %d", code)
	}
	if !strings.Contains(stderr, "no transfer matches") {
		t.Fatalf("the error is %q", stderr)
	}
}

// TestHistoryApply checks that an item a device held back reaches the clipboard
// when the user applies it.
func TestHistoryApply(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 44000, api.Options{})
	other := newDevice(t, network, bus, 45000, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)
	other.pair(t, device)
	waitOnline(t, device, other)

	if err := other.Trust().SetSelfRequireConfirmation(t.Context(), trust.ScopeClipboard, true); err != nil {
		t.Fatalf("set the confirmation flag: %v", err)
	}
	device.emit(t, "held item")

	id := waitItemID(t, other, "held item")
	if _, applied := other.clipboard.LastWrite(); applied {
		t.Fatal("the item reached the clipboard without the user")
	}

	code, stdout, stderr := runCommand(t, "", "history", "apply", id, "--endpoint", other.endpoint)
	if code != exitOK {
		t.Fatalf("history apply exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "applied the item") {
		t.Fatalf("the output is %q", stdout)
	}
	content, applied := other.clipboard.LastWrite()
	if !applied {
		t.Fatal("the clipboard did not change")
	}
	if text := clipText(t, content); text != "held item" {
		t.Fatalf("the clipboard holds %q", text)
	}
}

// waitItemID waits until the history of the device lists the item and returns
// its id.
func waitItemID(t *testing.T, d *testDevice, summary string) string {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		code, stdout, stderr := runCommand(t, "", "--endpoint", d.endpoint, "history")
		if code != exitOK {
			t.Fatalf("history exited with %d, stderr %q", code, stderr)
		}
		for _, line := range strings.Split(stdout, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 || fields[0] == "ID" {
				continue
			}
			if strings.Contains(line, summary) {
				return fields[0]
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no history item with %q within %v", summary, testTimeout)
	return ""
}

// clipText returns the plain text of the clipboard content.
func clipText(t *testing.T, content ports.Content) string {
	t.Helper()

	for _, representation := range content.Representations {
		if representation.MIME == clip.MIMETextPlain {
			return string(representation.Data)
		}
	}
	t.Fatal("the clipboard content has no plain text")
	return ""
}
