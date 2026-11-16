package main

import (
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// settleTime is how long a test watches a device that has to stay offline.
const settleTime = 500 * time.Millisecond

// TestDevicesAddress connects two paired daemons without discovery: the address
// of the peer is entered by the user.
func TestDevicesAddress(t *testing.T) {
	network := vnet.NewNetwork()
	device := newDevice(t, network, fake.NewBus(), 700, api.Options{})
	other := newDevice(t, network, fake.NewBus(), 710, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)
	other.pair(t, device)

	code, stdout, stderr := runCommand(t, "",
		"--endpoint", device.endpoint, "devices", "address", other.name, other.Addr().String())
	if code != exitOK {
		t.Fatalf("devices address exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"address " + other.Addr().String(), "saved for", "the device is online"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the output has no %q:\n%s", want, stdout)
		}
	}
	waitOnline(t, device, other)
	waitOnline(t, other, device)
}

// TestDevicesAddressAfterRestart reconnects the devices that were paired before
// a restart.
func TestDevicesAddressAfterRestart(t *testing.T) {
	network := vnet.NewNetwork()
	oneBus, twoBus := fake.NewBus(), fake.NewBus()
	oneKeys, twoKeys := fake.NewKeystore(), fake.NewKeystore()
	oneDir, twoDir := t.TempDir(), t.TempDir()

	one := newDeviceIn(t, network, oneBus, 720, oneDir, oneKeys, api.Options{})
	two := newDeviceIn(t, network, twoBus, 730, twoDir, twoKeys, api.Options{})
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	oneID, twoID := one.DeviceID(), two.DeviceID()
	addr := two.Addr().String()
	stopDevice(t, one)
	stopDevice(t, two)

	one = newDeviceIn(t, network, oneBus, 720, oneDir, oneKeys, api.Options{})
	two = newDeviceIn(t, network, twoBus, 730, twoDir, twoKeys, api.Options{})
	one.start(t)
	two.start(t)
	requireOffline(t, one, twoID)
	requireOffline(t, two, oneID)

	code, stdout, stderr := runCommand(t, "",
		"--endpoint", one.endpoint, "devices", "address", twoID.String(), addr)
	if code != exitOK {
		t.Fatalf("devices address exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "the device is online") {
		t.Errorf("the output does not report the device:\n%s", stdout)
	}
	waitOnline(t, one, two)
	waitOnline(t, two, one)
}

// TestDevicesAddressJSON checks the machine readable answer.
func TestDevicesAddressJSON(t *testing.T) {
	network := vnet.NewNetwork()
	device := newDevice(t, network, fake.NewBus(), 740, api.Options{})
	other := newDevice(t, network, fake.NewBus(), 750, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)

	code, stdout, stderr := runCommand(t, "",
		"--endpoint", device.endpoint, "--json", "devices", "address", "--wait", "0s",
		other.name, other.Addr().String())
	if code != exitOK {
		t.Fatalf("devices address exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{`"deviceId"`, other.DeviceID().String()} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the answer has no %q:\n%s", want, stdout)
		}
	}
}

// TestDevicesAddressUsage checks the refused command lines and addresses.
func TestDevicesAddressUsage(t *testing.T) {
	network := vnet.NewNetwork()
	device := newDevice(t, network, fake.NewBus(), 760, api.Options{})
	device.start(t)

	lines := [][]string{
		{"devices", "address"},
		{"devices", "address", "device"},
		{"devices", "address", "device", ""},
		{"devices", "address", "device", "localhost"},
		{"devices", "address", "device", "127.0.0.1"},
		{"devices", "address", "device", "127.0.0.1:"},
		{"devices", "address", "device", "127.0.0.1:0"},
		{"devices", "address", "device", "127.0.0.1:65536"},
		{"devices", "address", "device", "::1:5000"},
		{"devices", "address", "no such device", "127.0.0.1:5000"},
	}
	for _, args := range lines {
		code, _, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, args...)...)
		if code != exitFailure {
			t.Errorf("%v exited with %d", args, code)
		}
		if stderr == "" {
			t.Errorf("%v reported no error", args)
		}
	}
}

// stopDevice stops a device before the test creates it again.
func stopDevice(t *testing.T, d *testDevice) {
	t.Helper()

	ctx := t.Context()
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("stop node: %v", err)
	}
}

// requireOffline checks that the device has no session with the peer, so a later
// connection is the work of the test.
func requireOffline(t *testing.T, d *testDevice, id model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(settleTime)
	for time.Now().Before(deadline) {
		state, err := d.Peer(t.Context(), id)
		if err != nil {
			t.Fatalf("read the peer: %v", err)
		}
		if state.Online {
			t.Fatalf("the device opened a session with %s on its own", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
