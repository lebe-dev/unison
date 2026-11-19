package api_test

import (
	"testing"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestSetDeviceRemoteSettings checks the permission of a paired device to
// change the confirmation flags of this device.
func TestSetDeviceRemoteSettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 44500)
	n.start(t)
	peer := offlinePeer(t, 7)
	if err := n.AddPeer(t.Context(), peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	list, err := client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		t.Fatalf("list the devices: %v", err)
	}
	if len(list.Msg.GetDevices()) != 1 {
		t.Fatalf("the list has %d devices, want one", len(list.Msg.GetDevices()))
	}
	if !list.Msg.GetDevices()[0].GetAllowRemoteSettings() {
		t.Fatal("the device may not change the settings by default")
	}

	if _, err := client.SetDeviceRemoteSettings(ctx, connect.NewRequest(&localv1.SetDeviceRemoteSettingsRequest{
		DeviceId: peer.DeviceID().String(),
		Allow:    false,
	})); err != nil {
		t.Fatalf("deny the device: %v", err)
	}
	list, err = client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		t.Fatalf("list the devices: %v", err)
	}
	if list.Msg.GetDevices()[0].GetAllowRemoteSettings() {
		t.Fatal("the device may still change the settings")
	}

	if _, err := client.SetDeviceRemoteSettings(ctx, connect.NewRequest(&localv1.SetDeviceRemoteSettingsRequest{
		DeviceId: peer.DeviceID().String(),
		Allow:    true,
	})); err != nil {
		t.Fatalf("allow the device: %v", err)
	}
	trusted, err := n.Trust().Get(ctx, peer.DeviceID())
	if err != nil {
		t.Fatalf("read the device: %v", err)
	}
	if trusted.RemoteSettingsDenied {
		t.Fatal("the permission is still denied")
	}
}

// TestSetDeviceRemoteSettingsInvalid checks the refused requests.
func TestSetDeviceRemoteSettingsInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 44600)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	_, err := client.SetDeviceRemoteSettings(ctx, connect.NewRequest(&localv1.SetDeviceRemoteSettingsRequest{}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.SetDeviceRemoteSettings(ctx, connect.NewRequest(&localv1.SetDeviceRemoteSettingsRequest{
		DeviceId: n.DeviceID().String(),
	}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.SetDeviceRemoteSettings(ctx, connect.NewRequest(&localv1.SetDeviceRemoteSettingsRequest{
		DeviceId: offlinePeer(t, 8).DeviceID().String(),
	}))
	requireCode(t, err, connect.CodeNotFound)
}
