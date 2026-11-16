package main

import (
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/trust"
)

// TestPairTwoDevices runs the interactive pairing of two daemons: both show the
// same code, both confirm it and both store the peer.
func TestPairTwoDevices(t *testing.T) {
	network, bus := newNetwork(t)
	one := newDevice(t, network, bus, 400, api.Options{})
	two := newDevice(t, network, bus, 410, api.Options{})
	one.start(t)
	two.start(t)

	first := startSession(t, "--endpoint", one.endpoint, "pair")
	first.waitOutput(t, first.stdout, "pairing window open")

	second := startSession(t, "--endpoint", two.endpoint, "pair")
	second.waitOutput(t, second.stdout, "pairing window open")
	second.waitOutput(t, second.stderr, "choose a device")
	second.answer(t, "1")

	firstOutput := first.waitOutput(t, first.stdout, "code")
	secondOutput := second.waitOutput(t, second.stdout, "code")
	firstCode, secondCode := codeOf(t, firstOutput), codeOf(t, secondOutput)
	if firstCode != secondCode {
		t.Fatalf("the devices show different codes: %s and %s", firstCode, secondCode)
	}

	first.answer(t, "y")
	second.answer(t, "y")

	if code := first.wait(t); code != exitOK {
		t.Fatalf("the first device exited with %d, stdout %q, stderr %q",
			code, first.stdout.String(), first.stderr.String())
	}
	if code := second.wait(t); code != exitOK {
		t.Fatalf("the second device exited with %d, stdout %q, stderr %q",
			code, second.stdout.String(), second.stderr.String())
	}
	for _, output := range []string{first.stdout.String(), second.stdout.String()} {
		if !strings.Contains(output, "paired with") {
			t.Errorf("the output does not report the paired device:\n%s", output)
		}
	}

	peers := peersOf(t, one)
	if _, ok := peers[two.DeviceID()]; !ok {
		t.Errorf("the first device did not store the second one")
	}
	peers = peersOf(t, two)
	if _, ok := peers[one.DeviceID()]; !ok {
		t.Errorf("the second device did not store the first one")
	}
}

// TestPairRejected checks a code the user does not confirm.
func TestPairRejected(t *testing.T) {
	network, bus := newNetwork(t)
	one := newDevice(t, network, bus, 420, api.Options{})
	two := newDevice(t, network, bus, 430, api.Options{})
	one.start(t)
	two.start(t)

	first := startSession(t, "--endpoint", one.endpoint, "pair")
	first.waitOutput(t, first.stdout, "pairing window open")

	second := startSession(t, "--endpoint", two.endpoint, "pair")
	second.waitOutput(t, second.stderr, "choose a device")
	second.answer(t, "1")

	first.waitOutput(t, first.stdout, "code")
	second.waitOutput(t, second.stdout, "code")

	first.answer(t, "y")
	second.answer(t, "n")

	if code := second.wait(t); code != exitFailure {
		t.Fatalf("the rejecting device exited with %d, stderr %q", code, second.stderr.String())
	}
	if code := first.wait(t); code != exitFailure {
		t.Fatalf("the other device exited with %d, stderr %q", code, first.stderr.String())
	}
}

// TestPairWindowBusy checks the second window of a daemon.
func TestPairWindowBusy(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 440, api.Options{})
	device.start(t)

	first := startSession(t, "--endpoint", device.endpoint, "pair")
	first.waitOutput(t, first.stdout, "pairing window open")

	code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "pair")
	if code != exitFailure {
		t.Fatalf("the second window exited with %d", code)
	}
	if !strings.Contains(stderr, "failed_precondition") {
		t.Errorf("the error does not report the open window:\n%s", stderr)
	}
}

// peersOf returns the paired devices of a test device by their ID.
func peersOf(t *testing.T, d *testDevice) map[model.DeviceID]trust.Peer {
	t.Helper()

	peers, err := d.Trust().List(t.Context(), trust.ListFilter{})
	if err != nil {
		t.Fatalf("list the peers: %v", err)
	}
	out := make(map[model.DeviceID]trust.Peer, len(peers))
	for _, peer := range peers {
		out[peer.DeviceID()] = peer
	}
	return out
}

// TestPairQrCode checks the code for a phone: the command prints it, a device
// that scans it asks for the decision of the user and both devices end up
// paired.
func TestPairQrCode(t *testing.T) {
	network, bus := newNetwork(t)
	showing := newDevice(t, network, bus, 450, api.Options{})
	scanner := newDevice(t, network, bus, 460, api.Options{})
	showing.start(t)
	scanner.start(t)

	session := startSession(t, "--endpoint", showing.endpoint, "pair")
	output := session.waitOutput(t, session.stdout, "qr: ")
	uri := qrOf(t, output)
	code, err := pairing.ParseQR(uri)
	if err != nil {
		t.Fatalf("parse the code: %v", err)
	}
	if code.DeviceID != showing.DeviceID() {
		t.Errorf("code device = %s, want %s", code.DeviceID, showing.DeviceID())
	}
	if code.Role != model.RolePersonal {
		t.Errorf("code role = %s, want personal", code.Role)
	}
	if !strings.Contains(output, "█") {
		t.Errorf("the output has no drawing of the code:\n%s", output)
	}

	if err := scanner.ScanPairingQR(uri); err != nil {
		t.Fatalf("scan the code: %v", err)
	}
	session.waitOutput(t, session.stderr, "add the device?")
	session.answer(t, "y")
	if exit := session.wait(t); exit != exitOK {
		t.Fatalf("the command exited with %d, stderr %q", exit, session.stderr.String())
	}
	if !strings.Contains(session.stdout.String(), "paired with") {
		t.Errorf("the output does not report the paired device:\n%s", session.stdout.String())
	}

	if _, ok := peersOf(t, showing)[scanner.DeviceID()]; !ok {
		t.Error("the showing device did not store the other one")
	}
	if _, ok := peersOf(t, scanner)[showing.DeviceID()]; !ok {
		t.Error("the scanning device did not store the other one")
	}
}

// TestPairQrCodeJSON checks that the code stays out of the event stream.
func TestPairQrCodeJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 470, api.Options{})
	device.start(t)

	session := startSession(t, "--endpoint", device.endpoint, "--json", "pair")
	session.waitOutput(t, session.stderr, "qr: ")
	if strings.Contains(session.stdout.String(), "qr: ") {
		t.Errorf("the code is on the event stream:\n%s", session.stdout.String())
	}
}

// TestPairWithCode joins the device that shows a code from the command line:
// the code replaces the discovery, and the showing device confirms the device
// that joined.
func TestPairWithCode(t *testing.T) {
	network, bus := newNetwork(t)
	showing := newDevice(t, network, bus, 480, api.Options{})
	joining := newDevice(t, network, bus, 490, api.Options{})
	showing.start(t)
	joining.start(t)

	window := startSession(t, "--endpoint", showing.endpoint, "pair")
	uri := qrOf(t, window.waitOutput(t, window.stdout, "qr: "))

	joined := startSession(t, "--endpoint", joining.endpoint, "pair", "--qr", uri)
	joined.waitOutput(t, joined.stdout, "waiting for the other device to confirm")

	window.waitOutput(t, window.stderr, "add the device?")
	window.answer(t, "y")

	if code := joined.wait(t); code != exitOK {
		t.Fatalf("the joining device exited with %d, stdout %q, stderr %q",
			code, joined.stdout.String(), joined.stderr.String())
	}
	if code := window.wait(t); code != exitOK {
		t.Fatalf("the showing device exited with %d, stdout %q, stderr %q",
			code, window.stdout.String(), window.stderr.String())
	}
	if !strings.Contains(joined.stdout.String(), "paired with") {
		t.Errorf("the output does not report the paired device:\n%s", joined.stdout.String())
	}

	if _, ok := peersOf(t, joining)[showing.DeviceID()]; !ok {
		t.Error("the joining device did not store the other one")
	}
	if _, ok := peersOf(t, showing)[joining.DeviceID()]; !ok {
		t.Error("the showing device did not store the other one")
	}
}

// qrOf returns the pairing code of the output of pair.
func qrOf(t *testing.T, output string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		if uri, ok := strings.CutPrefix(line, "qr: "); ok {
			return uri
		}
	}
	t.Fatalf("no pairing code in the output %q", output)
	return ""
}
