package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestStatus checks the human readable status.
func TestStatus(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 100, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "status")
	if code != exitOK {
		t.Fatalf("status exited with %d, stderr %q", code, stderr)
	}
	fingerprint := fingerprintOf(device.DeviceID().String())
	for _, want := range []string{testVersion + " running", device.name, fingerprint, "online", "clipboard"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status does not report %q:\n%s", want, stdout)
		}
	}
}

// TestStatusJSON checks the machine readable status.
func TestStatusJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 110, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "--json", "status")
	if code != exitOK {
		t.Fatalf("status exited with %d, stderr %q", code, stderr)
	}

	var status localv1.GetStatusResponse
	if err := protojson.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode the status %q: %v", stdout, err)
	}
	if status.GetVersion() != testVersion {
		t.Errorf("version is %q, want %q", status.GetVersion(), testVersion)
	}
	if status.GetDeviceId() != device.DeviceID().String() {
		t.Errorf("device id is %q, want %q", status.GetDeviceId(), device.DeviceID().String())
	}
	if !status.GetRunning() {
		t.Error("the status reports a stopped daemon")
	}
	if !status.GetNetwork().GetOnline() {
		t.Error("the status reports an offline network")
	}
}

// TestStatusStopped checks the status of a node that has not started.
func TestStatusStopped(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 120, api.Options{})

	code, stdout, _ := runCommand(t, "", "--endpoint", device.endpoint, "status")
	if code != exitOK {
		t.Fatalf("status exited with %d", code)
	}
	if !strings.Contains(stdout, "stopped") {
		t.Errorf("the status does not report a stopped daemon:\n%s", stdout)
	}
}

// TestStatusUnreachable checks the exit code of a daemon that is not there.
func TestStatusUnreachable(t *testing.T) {
	endpoint, err := localconn.EndpointIn(shortDir(t), "missing.sock")
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}

	code, stdout, stderr := runCommand(t, "", "--endpoint", endpoint, "status")
	if code != exitUnreachable {
		t.Fatalf("status exited with %d, want %d, stderr %q", code, exitUnreachable, stderr)
	}
	if !strings.Contains(stderr, "not reachable") {
		t.Errorf("the error does not name the endpoint:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("the command wrote to stdout: %q", stdout)
	}
}

// TestDevices checks the device list in both forms.
func TestDevices(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 200, api.Options{})
	other := newDevice(t, network, bus, 210, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices")
	if code != exitOK {
		t.Fatalf("devices exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"FINGERPRINT", other.name, "personal", "macos"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the device list does not report %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, fingerprintOf(other.DeviceID().String())) {
		t.Errorf("the device list has no fingerprint:\n%s", stdout)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "devices", "ls", "--json")
	if code != exitOK {
		t.Fatalf("devices ls exited with %d, stderr %q", code, stderr)
	}
	var list localv1.ListDevicesResponse
	if err := protojson.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("decode the devices %q: %v", stdout, err)
	}
	if len(list.GetDevices()) != 1 {
		t.Fatalf("the list has %d devices, want one", len(list.GetDevices()))
	}
	if got := list.GetDevices()[0].GetDeviceId(); got != other.DeviceID().String() {
		t.Errorf("device id is %q, want %q", got, other.DeviceID().String())
	}
}

// TestDevicesEmpty checks the list without paired devices.
func TestDevicesEmpty(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 220, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices")
	if code != exitOK {
		t.Fatalf("devices exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "no paired devices") {
		t.Errorf("the list is not reported as empty:\n%s", stdout)
	}
}

// TestDevicesRenameResolution checks that a device is found by its name and by
// its fingerprint and renamed locally.
func TestDevicesRenameResolution(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 230, api.Options{})
	other := newDevice(t, network, bus, 240, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "rename", other.name, "renamed")
	if code != exitOK {
		t.Fatalf("rename exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "renamed") {
		t.Errorf("the command does not report the new name:\n%s", stdout)
	}
	peer, err := device.Trust().Get(t.Context(), other.DeviceID())
	if err != nil {
		t.Fatalf("read the device: %v", err)
	}
	if peer.Name != "renamed" {
		t.Errorf("the stored name is %q, want %q", peer.Name, "renamed")
	}

	fingerprint := fingerprintOf(other.DeviceID().String())
	prefix := strings.SplitN(fingerprint, "-", 2)[0]
	for _, reference := range []string{prefix, strings.ToLower(prefix), other.DeviceID().String()[:8]} {
		code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "rename", reference, "again")
		if code != exitOK {
			t.Fatalf("%q: rename exited with %d, stdout %q, stderr %q", reference, code, stdout, stderr)
		}
	}
	peer, err = device.Trust().Get(t.Context(), other.DeviceID())
	if err != nil {
		t.Fatalf("read the device: %v", err)
	}
	if peer.Name != "again" {
		t.Errorf("the stored name is %q, want %q", peer.Name, "again")
	}
	if other.Name() != other.name {
		t.Errorf("the name of the peer changed to %q", other.Name())
	}
}

// TestDevicesRenameSelf checks the name of this device.
func TestDevicesRenameSelf(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 235, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "--json", "devices", "rename", "self", "laptop")
	if code != exitOK {
		t.Fatalf("rename exited with %d, stderr %q", code, stderr)
	}
	var response localv1.RenameDeviceResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the response %q: %v", stdout, err)
	}
	if response.GetDevice().GetName() != "laptop" {
		t.Errorf("the response names the device %q", response.GetDevice().GetName())
	}
	if device.Name() != "laptop" {
		t.Errorf("the device runs as %q, want %q", device.Name(), "laptop")
	}
}

// TestDevicesConfirmSelf checks the confirmation flags of the local device.
func TestDevicesConfirmSelf(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 320, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "confirm", "self", "on")
	if code != exitOK {
		t.Fatalf("devices confirm exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "clipboard confirmation for every device is on") {
		t.Errorf("the command does not report the flag:\n%s", stdout)
	}
	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "devices", "confirm", "--files", "self", "on")
	if code != exitOK {
		t.Fatalf("devices confirm --files exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "files confirmation for every device is on") {
		t.Errorf("the command does not report the files flag:\n%s", stdout)
	}
	flags, err := device.Trust().SelfConfirmations(t.Context())
	if err != nil {
		t.Fatalf("read the flags: %v", err)
	}
	if flags != (trust.Confirmations{Clipboard: true, Files: true}) {
		t.Fatalf("the flags are %+v, want both on", flags)
	}

	if code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "confirm", "self", "off"); code != exitOK {
		t.Fatalf("devices confirm off exited with %d, stderr %q", code, stderr)
	}
	flags, err = device.Trust().SelfConfirmations(t.Context())
	if err != nil {
		t.Fatalf("read the flags: %v", err)
	}
	if flags != (trust.Confirmations{Files: true}) {
		t.Fatalf("the flags are %+v, want only files", flags)
	}
}

// TestDevicesConfirmPeer checks both flags of a pair: the one of this device
// and the one of the paired device.
func TestDevicesConfirmPeer(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 330, api.Options{})
	other := newDevice(t, network, bus, 340, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)
	other.pair(t, device)
	waitOnline(t, device, other)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "confirm", "--files", other.name, "on")
	if code != exitOK {
		t.Fatalf("devices confirm exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, fingerprintOf(other.DeviceID().String())) {
		t.Errorf("the command does not report the device:\n%s", stdout)
	}
	waitFlags(t, device, other.DeviceID(), func(peer trust.Peer) bool { return peer.Confirm.Files })
	waitFlags(t, other, device.DeviceID(), func(peer trust.Peer) bool { return peer.ConfirmRemote.Files })

	code, _, stderr = runCommand(t, "", "--endpoint", device.endpoint, "devices", "confirm", "--peer", other.name, "on")
	if code != exitOK {
		t.Fatalf("devices confirm --peer exited with %d, stderr %q", code, stderr)
	}
	waitFlags(t, other, device.DeviceID(), func(peer trust.Peer) bool { return peer.Confirm.Clipboard })
	waitFlags(t, device, other.DeviceID(), func(peer trust.Peer) bool { return peer.ConfirmRemote.Clipboard })
}

// waitFlags waits until the record of a peer on the device satisfies ok.
func waitFlags(t *testing.T, d *testDevice, peer model.DeviceID, ok func(trust.Peer) bool) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		record, err := d.Trust().Get(t.Context(), peer)
		if err != nil {
			t.Fatalf("read the peer: %v", err)
		}
		if ok(record) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the flags of %s did not change within %v", peer, testTimeout)
}

// TestDevicesConfirmUsage checks the arguments of the command.
func TestDevicesConfirmUsage(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 350, api.Options{})
	device.start(t)

	for _, args := range [][]string{
		{"devices", "confirm"},
		{"devices", "confirm", "self"},
		{"devices", "confirm", "self", "maybe"},
		{"devices", "confirm", "--peer", "self", "on"},
	} {
		code, _, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, args...)...)
		if code != exitFailure {
			t.Fatalf("%v exited with %d", args, code)
		}
		if stderr == "" {
			t.Errorf("%v reported no error", args)
		}
	}
}

// TestDevicesRevoke checks the revocation of a paired device.
func TestDevicesRevoke(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 360, api.Options{})
	other := newDevice(t, network, bus, 370, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)

	code, stdout, stderr := runCommand(t, "y\n", "--endpoint", device.endpoint, "devices", "revoke", other.name)
	if code != exitOK {
		t.Fatalf("devices revoke exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, fingerprintOf(other.DeviceID().String())) {
		t.Errorf("the command does not report the device:\n%s", stdout)
	}
	peer, err := device.Trust().Get(t.Context(), other.DeviceID())
	if err != nil {
		t.Fatalf("read the revoked device: %v", err)
	}
	if !peer.Revoked {
		t.Fatal("the device is not revoked")
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "devices")
	if code != exitOK {
		t.Fatalf("devices exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "no paired devices") {
		t.Errorf("the revoked device is still listed:\n%s", stdout)
	}
}

// TestDevicesRevokeDeclined checks that a declined revocation changes nothing.
func TestDevicesRevokeDeclined(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 380, api.Options{})
	other := newDevice(t, network, bus, 390, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)

	code, stdout, stderr := runCommand(t, "n\n", "--endpoint", device.endpoint, "devices", "revoke", other.name)
	if code != exitOK {
		t.Fatalf("devices revoke exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "nothing was revoked") {
		t.Errorf("the command does not report the refusal:\n%s", stdout)
	}
	peer, err := device.Trust().Get(t.Context(), other.DeviceID())
	if err != nil {
		t.Fatalf("read the device: %v", err)
	}
	if peer.Revoked {
		t.Fatal("the device was revoked")
	}
}

// TestDevicesRevokeSelf checks that the local device leaves the group.
func TestDevicesRevokeSelf(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 400, api.Options{})
	other := newDevice(t, network, bus, 410, api.Options{})
	device.start(t)
	other.start(t)
	device.pair(t, other)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "revoke", "self", "--yes")
	if code != exitOK {
		t.Fatalf("devices revoke exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "left the group") {
		t.Errorf("the command does not report leaving the group:\n%s", stdout)
	}
	peers, err := device.Trust().List(t.Context(), trust.ListFilter{IncludeRevoked: true})
	if err != nil {
		t.Fatalf("list the devices: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("the device keeps %d devices of the group", len(peers))
	}
}

// TestDevicesRevokeUsage checks the arguments of the command.
func TestDevicesRevokeUsage(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 420, api.Options{})
	device.start(t)

	for _, args := range [][]string{
		{"devices", "revoke"},
		{"devices", "revoke", "one", "two"},
	} {
		code, _, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, args...)...)
		if code != exitFailure {
			t.Fatalf("%v exited with %d", args, code)
		}
		if stderr == "" {
			t.Errorf("%v reported no error", args)
		}
	}
}

// TestDevicesRenameUnknown checks a reference that no device matches.
func TestDevicesRenameUnknown(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 250, api.Options{})
	device.start(t)

	code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "rename", "phone", "work")
	if code != exitFailure {
		t.Fatalf("rename exited with %d", code)
	}
	if !strings.Contains(stderr, "no device matches") {
		t.Errorf("the error does not report the unknown device:\n%s", stderr)
	}
}

// TestDevicesRenameAmbiguous checks a name that several devices share.
func TestDevicesRenameAmbiguous(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 260, api.Options{})
	first := newDevice(t, network, bus, 270, api.Options{})
	second := newDevice(t, network, bus, 280, api.Options{})
	device.start(t)
	first.start(t)
	second.start(t)
	device.addPeer(t, first, "phone")
	device.addPeer(t, second, "phone")

	code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "devices", "rename", "phone", "work")
	if code != exitFailure {
		t.Fatalf("rename exited with %d", code)
	}
	if !strings.Contains(stderr, "matches 2 devices") {
		t.Errorf("the error does not report the ambiguous name:\n%s", stderr)
	}
}

// TestClipGet checks the text of the last item.
func TestClipGet(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 300, api.Options{})
	device.start(t)
	device.emit(t, "hello clipboard")

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "clip", "get")
	if code != exitOK {
		t.Fatalf("clip get exited with %d, stderr %q", code, stderr)
	}
	if stdout != "hello clipboard" {
		t.Errorf("clip get printed %q", stdout)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "clip", "get", "--json")
	if code != exitOK {
		t.Fatalf("clip get --json exited with %d, stderr %q", code, stderr)
	}
	var response localv1.GetClipResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the item %q: %v", stdout, err)
	}
	if response.GetItem().GetId() == "" {
		t.Error("the item has no id")
	}
	if plaintext, _ := plainText(response.GetItem()); plaintext != "hello clipboard" {
		t.Errorf("the item carries %q", plaintext)
	}
}

// TestClipGetEmpty checks the empty history.
func TestClipGetEmpty(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 310, api.Options{})
	device.start(t)

	code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "clip", "get")
	if code != exitFailure {
		t.Fatalf("clip get exited with %d", code)
	}
	if !strings.Contains(stderr, "history is empty") {
		t.Errorf("the error does not report the empty history:\n%s", stderr)
	}
}

// TestHistoryFetchFlags checks that the output path may follow the item id.
func TestHistoryFetchFlags(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 315, api.Options{})
	device.start(t)
	device.emit(t, "a fetchable item")

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "history", "ls")
	if code != exitOK {
		t.Fatalf("history exited with %d, stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("the history lists no items:\n%s", stdout)
	}
	id := strings.Fields(lines[1])[0]

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "history", "fetch", id, "-o", t.TempDir())
	if code != exitOK {
		t.Fatalf("history fetch exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "saved") {
		t.Errorf("history fetch printed %q", stdout)
	}
}

// TestClipSet checks that the standard input reaches the pipeline.
func TestClipSet(t *testing.T) {
	network, bus := newNetwork(t)
	local := &fakeLocalClip{}
	device := newDevice(t, network, bus, 320, api.Options{LocalClip: local})
	device.start(t)

	code, stdout, stderr := runCommand(t, "hello", "--endpoint", device.endpoint, "clip", "set")
	if code != exitOK {
		t.Fatalf("clip set exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "clipboard updated (5 bytes)") {
		t.Errorf("clip set printed %q", stdout)
	}
	content := local.submitted()
	if len(content.Representations) != 1 {
		t.Fatalf("the pipeline received %d representations, want one", len(content.Representations))
	}
	if got := string(content.Representations[0].Data); got != "hello" {
		t.Errorf("the pipeline received %q", got)
	}
	if got := content.Representations[0].MIME; got != clip.MIMETextPlain {
		t.Errorf("the content type is %q, want %q", got, clip.MIMETextPlain)
	}

	code, _, stderr = runCommand(t, "dash", "--endpoint", device.endpoint, "clip", "set", "-")
	if code != exitOK {
		t.Fatalf("clip set - exited with %d, stderr %q", code, stderr)
	}
	if got := string(local.submitted().Representations[0].Data); got != "dash" {
		t.Errorf("the pipeline received %q", got)
	}
}

// TestClipSetJSON checks the machine readable answer of a set.
func TestClipSetJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 330, api.Options{LocalClip: &fakeLocalClip{}})
	device.start(t)

	code, stdout, stderr := runCommand(t, "hi", "--endpoint", device.endpoint, "clip", "set", "--json")
	if code != exitOK {
		t.Fatalf("clip set exited with %d, stderr %q", code, stderr)
	}
	var response localv1.SetClipResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the answer %q: %v", stdout, err)
	}
	if response.GetItem().GetSeq() != 7 {
		t.Errorf("the item has seq %d", response.GetItem().GetSeq())
	}
	representations := response.GetItem().GetRepresentations()
	if len(representations) != 1 {
		t.Fatalf("the item has %d representations, want one", len(representations))
	}
	if len(representations[0].GetInlineBytes()) != 0 {
		t.Error("the answer repeats the text it was given")
	}
	if representations[0].GetSize() != 2 {
		t.Errorf("the representation has size %d, want 2", representations[0].GetSize())
	}
}

// TestClipSetEmpty checks that empty input is refused.
func TestClipSetEmpty(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 340, api.Options{LocalClip: &fakeLocalClip{}})
	device.start(t)

	code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "clip", "set")
	if code != exitFailure {
		t.Fatalf("clip set exited with %d", code)
	}
	if !strings.Contains(stderr, "empty") {
		t.Errorf("the error does not report the empty input:\n%s", stderr)
	}
}

// TestClipSetTooLong checks that oversized input is refused before it reaches
// the daemon.
func TestClipSetTooLong(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 350, api.Options{LocalClip: &fakeLocalClip{}})
	device.start(t)

	code, _, stderr := runCommand(t, strings.Repeat("a", clip.MaxInlineText+1), "--endpoint", device.endpoint, "clip", "set")
	if code != exitFailure {
		t.Fatalf("clip set exited with %d", code)
	}
	if !strings.Contains(stderr, "longer than") {
		t.Errorf("the error does not report the size:\n%s", stderr)
	}
}

// TestClipSetUnavailable checks the daemon that serves no clipboard submit.
func TestClipSetUnavailable(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 360, api.Options{})
	device.start(t)

	code, _, stderr := runCommand(t, "hi", "--endpoint", device.endpoint, "clip", "set")
	if code != exitFailure {
		t.Fatalf("clip set exited with %d", code)
	}
	if !strings.Contains(stderr, "unimplemented") {
		t.Errorf("the error does not come from the daemon:\n%s", stderr)
	}
}

// TestPause checks the pause states.
func TestPause(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 370, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "pause")
	if code != exitOK || !strings.Contains(stdout, "on") {
		t.Fatalf("pause exited with %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "pause", "5m")
	if code != exitOK {
		t.Fatalf("pause 5m exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "paused until") {
		t.Errorf("pause 5m printed %q", stdout)
	}

	for _, args := range [][]string{
		{"--json", "pause", "5m"},
		{"pause", "5m", "--json"},
	} {
		code, stdout, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, args...)...)
		if code != exitOK {
			t.Fatalf("%v exited with %d, stderr %q", args, code, stderr)
		}
		var response localv1.SetPauseResponse
		if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
			t.Fatalf("%v printed %q: %v", args, stdout, err)
		}
		until := time.UnixMilli(response.GetPause().GetUntilUnixMs())
		if delta := time.Until(until); delta < 4*time.Minute || delta > 6*time.Minute {
			t.Errorf("%v: the pause ends in %v, want about five minutes", args, delta)
		}
	}

	code, stdout, _ = runCommand(t, "", "--endpoint", device.endpoint, "pause", "on")
	if code != exitOK || !strings.Contains(stdout, "turned off") {
		t.Fatalf("pause on exited with %d, stdout %q", code, stdout)
	}

	code, stdout, _ = runCommand(t, "", "--endpoint", device.endpoint, "pause", "off")
	if code != exitOK || !strings.Contains(stdout, "on") {
		t.Fatalf("pause off exited with %d, stdout %q", code, stdout)
	}
}

// TestPauseInvalid checks a pause argument that is not a duration.
func TestPauseInvalid(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 380, api.Options{})
	device.start(t)

	for _, argument := range []string{"later", "0s"} {
		code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "pause", argument)
		if code != exitFailure {
			t.Fatalf("%q: pause exited with %d", argument, code)
		}
		if !strings.Contains(stderr, "unknown pause") {
			t.Errorf("%q: the error is not reported:\n%s", argument, stderr)
		}
	}
}

// TestCommonFlagsBeforeSubcommand checks that the common flags work before the
// subcommand of a command as well as after it.
func TestCommonFlagsBeforeSubcommand(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 490, api.Options{})
	device.start(t)

	commands := []string{"devices", "transfers", "requests", "history", "settings", "relay"}
	for _, command := range commands {
		code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, command, "--json")
		if code != exitOK {
			t.Fatalf("%s --json exited with %d, stderr %q", command, code, stderr)
		}
		if !json.Valid([]byte(stdout)) {
			t.Errorf("%s --json printed no JSON:\n%s", command, stdout)
		}
	}
}

// TestUsage checks the command line errors.
func TestUsage(t *testing.T) {
	cases := [][]string{
		{},
		{"unknown"},
		{"clip"},
		{"clip", "unknown"},
		{"devices", "rename", "only-one"},
		{"status", "extra"},
		{"--json"},
	}
	for _, args := range cases {
		code, _, _ := runCommand(t, "", args...)
		if code != exitFailure {
			t.Errorf("%v exited with %d, want %d", args, code, exitFailure)
		}
	}
}

// TestVersion checks the version flag.
func TestVersion(t *testing.T) {
	code, stdout, stderr := runCommand(t, "", "--version")
	if code != exitOK {
		t.Fatalf("--version exited with %d, stderr %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "unison ") {
		t.Errorf("--version printed %q", stdout)
	}
}
