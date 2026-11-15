package api_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestGetStatus checks the reported version, device and runtime state.
func TestGetStatus(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 100)
	n.start(t)
	client := newClient(t, n.Node, api.Options{Version: "1.2.3", DeviceName: "workstation"})

	ctx := callTimeout(t)
	response, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		t.Fatalf("get status: %v", err)
	}

	status := response.Msg
	if status.GetVersion() != "1.2.3" {
		t.Errorf("version is %q, want %q", status.GetVersion(), "1.2.3")
	}
	if status.GetDeviceId() != n.DeviceID().String() {
		t.Errorf("device id is %q, want %q", status.GetDeviceId(), n.DeviceID().String())
	}
	if status.GetDeviceName() != "workstation" {
		t.Errorf("device name is %q, want %q", status.GetDeviceName(), "workstation")
	}
	if !status.GetRunning() {
		t.Error("status reports a stopped node")
	}
	if !status.GetNetwork().GetOnline() {
		t.Error("status reports an offline node")
	}
	if status.GetPause().GetPaused() {
		t.Error("status reports a paused clipboard")
	}
	if len(status.GetDevices()) != 0 {
		t.Errorf("status reports %d devices, want none", len(status.GetDevices()))
	}
}

// TestGetStatusNotRunning checks that a node that has not started still answers.
func TestGetStatusNotRunning(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 200)
	client := newClient(t, n.Node, api.Options{Version: "0.1.0", DeviceName: "spare"})

	ctx := callTimeout(t)
	response, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		t.Fatalf("get status: %v", err)
	}

	status := response.Msg
	if status.GetRunning() {
		t.Error("status reports a running node")
	}
	if status.GetDeviceId() != "" {
		t.Errorf("device id is %q, want empty", status.GetDeviceId())
	}
	if status.GetDeviceName() != "spare" {
		t.Errorf("device name is %q, want %q", status.GetDeviceName(), "spare")
	}
}

// TestGetStatusDevices checks that paired devices are reported with their live
// state.
func TestGetStatusDevices(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 300)
	two := newNode(t, network, bus, 400)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	deadline := time.Now().Add(testTimeout)
	for {
		response, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
		if err != nil {
			t.Fatalf("get status: %v", err)
		}
		devices := response.Msg.GetDevices()
		if len(devices) != 1 {
			t.Fatalf("status reports %d devices, want one", len(devices))
		}
		device := devices[0]
		if device.GetDeviceId() != two.DeviceID().String() {
			t.Fatalf("device id is %q, want %q", device.GetDeviceId(), two.DeviceID().String())
		}
		if device.GetRole() != localv1.Role_ROLE_PERSONAL {
			t.Errorf("role is %s, want personal", device.GetRole())
		}
		if device.GetName() != "device-400" {
			t.Errorf("name is %q, want %q", device.GetName(), "device-400")
		}
		if device.GetOnline() {
			if device.GetPath() != localv1.Path_PATH_LAN {
				t.Errorf("path is %s, want lan", device.GetPath())
			}
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal("the peer did not come online")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWatchEventsPeerOnline checks that the event stream reports a peer that
// comes online.
func TestWatchEventsPeerOnline(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 500)
	two := newNode(t, network, bus, 600)
	one.start(t)
	two.start(t)

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	events := watch(t, client)

	one.pair(t, two)
	two.pair(t, one)

	event := waitEvent(t, events, localv1.EventKind_EVENT_KIND_PEER_ONLINE)
	if event.GetDeviceId() != two.DeviceID().String() {
		t.Errorf("device id is %q, want %q", event.GetDeviceId(), two.DeviceID().String())
	}
	if event.GetName() != "device-600" {
		t.Errorf("name is %q, want %q", event.GetName(), "device-600")
	}
	if event.GetRole() != localv1.Role_ROLE_PERSONAL {
		t.Errorf("role is %s, want personal", event.GetRole())
	}
	if event.GetPath() != localv1.Path_PATH_LAN {
		t.Errorf("path is %s, want lan", event.GetPath())
	}
	if event.GetAtUnixMs() == 0 {
		t.Error("event has no time")
	}
}

// TestWatchEventsClipboard checks that the event stream carries a captured
// clipboard item.
func TestWatchEventsClipboard(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 700)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	events := watch(t, client)

	emitClipboard(t, n, "hello")

	event := waitEvent(t, events, localv1.EventKind_EVENT_KIND_CLIPBOARD)
	if !event.GetLocal() {
		t.Error("the event of a captured item is not local")
	}
	item := event.GetItem()
	if item.GetOrigin() != n.DeviceID().String() {
		t.Errorf("origin is %q, want %q", item.GetOrigin(), n.DeviceID().String())
	}
	if textOf(t, item) != "hello" {
		t.Errorf("item text is %q, want %q", textOf(t, item), "hello")
	}
}

// TestWatchEventsNotRunning checks that the stream is refused by a stopped node.
func TestWatchEventsNotRunning(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 800)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})

	ctx := callTimeout(t)
	stream, err := client.WatchEvents(ctx, connect.NewRequest(&localv1.WatchEventsRequest{}))
	if err == nil {
		stream.Receive()
		err = stream.Err()
		_ = stream.Close()
	}
	requireCode(t, err, connect.CodeFailedPrecondition)
}

// TestDevicesNotImplemented checks the device calls the core does not serve yet.
func TestDevicesNotImplemented(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 900)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	id := model.DeviceID{1}.String()
	_, err := client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	requireCode(t, err, connect.CodeUnimplemented)

	_, err = client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{DeviceId: id, Name: "x"}))
	requireCode(t, err, connect.CodeUnimplemented)

	_, err = client.RevokeDevice(ctx, connect.NewRequest(&localv1.RevokeDeviceRequest{DeviceId: id}))
	requireCode(t, err, connect.CodeNotFound)

	_, err = client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{DeviceId: id}))
	requireCode(t, err, connect.CodeNotFound)
}

// TestRevokeDevice checks that a revoked device disappears from the status and
// that the call is idempotent.
func TestRevokeDevice(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 1000)
	two := newNode(t, network, bus, 1100)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)
	request := &localv1.RevokeDeviceRequest{DeviceId: two.DeviceID().String()}
	if _, err := client.RevokeDevice(ctx, connect.NewRequest(request)); err != nil {
		t.Fatalf("revoke the device: %v", err)
	}

	peer, err := one.Trust().Get(context.Background(), two.DeviceID())
	if err != nil {
		t.Fatalf("read the revoked device: %v", err)
	}
	if !peer.Revoked {
		t.Fatal("the device is not revoked")
	}

	status, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if len(status.Msg.GetDevices()) != 0 {
		t.Fatalf("status reports %d devices, want none", len(status.Msg.GetDevices()))
	}

	if _, err := client.RevokeDevice(ctx, connect.NewRequest(request)); err != nil {
		t.Fatalf("revoke the device twice: %v", err)
	}
}

// TestRevokeDeviceSelf checks that revoking the local device leaves the group.
func TestRevokeDeviceSelf(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 1200)
	two := newNode(t, network, bus, 1300)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)
	request := &localv1.RevokeDeviceRequest{DeviceId: one.DeviceID().String()}
	if _, err := client.RevokeDevice(ctx, connect.NewRequest(request)); err != nil {
		t.Fatalf("revoke the local device: %v", err)
	}

	peers, err := one.Trust().List(context.Background(), trust.ListFilter{IncludeRevoked: true})
	if err != nil {
		t.Fatalf("list the peers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("the device keeps %d peers of the group", len(peers))
	}
}

// TestRevokeDeviceInvalid checks the request without a usable device ID.
func TestRevokeDeviceInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1400)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	for _, id := range []string{"", "not-a-device"} {
		_, err := client.RevokeDevice(ctx, connect.NewRequest(&localv1.RevokeDeviceRequest{DeviceId: id}))
		requireCode(t, err, connect.CodeInvalidArgument)
	}
}
