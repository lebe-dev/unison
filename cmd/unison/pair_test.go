package main

import (
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
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
