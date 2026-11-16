package api_test

import (
	"context"
	"strings"
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

// TestListDevices checks that the paired devices are listed with their live
// state and the device that introduced them.
func TestListDevices(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 4000)
	two := newNode(t, network, bus, 4100)
	introducer := newNode(t, network, bus, 4200)
	one.start(t)
	two.start(t)
	introducer.start(t)
	one.pair(t, two)
	two.pair(t, one)

	added := trust.Peer{
		Public:       introducer.Identity().Public(),
		Name:         introducer.name,
		Platform:     model.PlatformMacOS,
		Role:         model.RolePersonal,
		IntroducedBy: two.DeviceID(),
	}
	if err := one.AddPeer(t.Context(), added); err != nil {
		t.Fatalf("add the introduced peer: %v", err)
	}

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)
	response, err := client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		t.Fatalf("list the devices: %v", err)
	}
	devices := response.Msg.GetDevices()
	if len(devices) != 2 {
		t.Fatalf("the list has %d devices, want two", len(devices))
	}

	byID := make(map[string]*localv1.Device, len(devices))
	for _, device := range devices {
		byID[device.GetDeviceId()] = device
	}
	direct := byID[two.DeviceID().String()]
	if direct == nil {
		t.Fatalf("the list has no paired device")
	}
	if direct.GetName() != two.name {
		t.Errorf("name is %q, want %q", direct.GetName(), two.name)
	}
	if direct.GetRole() != localv1.Role_ROLE_PERSONAL {
		t.Errorf("role is %s, want personal", direct.GetRole())
	}
	if direct.GetPlatform() != localv1.Platform_PLATFORM_MACOS {
		t.Errorf("platform is %s, want macos", direct.GetPlatform())
	}
	if direct.GetIntroducedBy() != "" {
		t.Errorf("a paired device reports introduced by %q", direct.GetIntroducedBy())
	}

	introduced := byID[introducer.DeviceID().String()]
	if introduced == nil {
		t.Fatalf("the list has no introduced device")
	}
	if introduced.GetIntroducedBy() != two.DeviceID().String() {
		t.Errorf("introduced by is %q, want %q", introduced.GetIntroducedBy(), two.DeviceID().String())
	}

	waitPeerOnline(t, one, two.DeviceID())
	response, err = client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		t.Fatalf("list the devices: %v", err)
	}
	for _, device := range response.Msg.GetDevices() {
		if device.GetDeviceId() != two.DeviceID().String() {
			continue
		}
		if !device.GetOnline() {
			t.Error("the paired device is listed offline")
		}
		if device.GetPath() != localv1.Path_PATH_LAN {
			t.Errorf("path is %s, want lan", device.GetPath())
		}
	}
}

// TestListDevicesRevoked checks that the list hides the revoked devices unless
// it is asked for them.
func TestListDevicesRevoked(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 4300)
	two := newNode(t, network, bus, 4400)
	one.start(t)
	two.start(t)
	one.pair(t, two)

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)
	if _, err := client.RevokeDevice(ctx, connect.NewRequest(&localv1.RevokeDeviceRequest{
		DeviceId: two.DeviceID().String(),
	})); err != nil {
		t.Fatalf("revoke the device: %v", err)
	}

	response, err := client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		t.Fatalf("list the devices: %v", err)
	}
	if len(response.Msg.GetDevices()) != 0 {
		t.Errorf("the list has %d devices, want none", len(response.Msg.GetDevices()))
	}

	response, err = client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{IncludeRevoked: true}))
	if err != nil {
		t.Fatalf("list the devices with the revoked ones: %v", err)
	}
	devices := response.Msg.GetDevices()
	if len(devices) != 1 {
		t.Fatalf("the list has %d devices, want one", len(devices))
	}
	if !devices[0].GetRevoked() {
		t.Error("the revoked device is listed as trusted")
	}
}

// TestRenameDevicePeer checks that the name of a peer changes locally and does
// not travel to it.
func TestRenameDevicePeer(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 4500)
	two := newNode(t, network, bus, 4600)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)
	response, err := client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
		DeviceId: two.DeviceID().String(),
		Name:     "  desk phone  ",
	}))
	if err != nil {
		t.Fatalf("rename the device: %v", err)
	}
	if response.Msg.GetDevice().GetName() != "desk phone" {
		t.Errorf("the response names the device %q", response.Msg.GetDevice().GetName())
	}

	peer, err := one.Trust().Get(ctx, two.DeviceID())
	if err != nil {
		t.Fatalf("read the renamed device: %v", err)
	}
	if peer.Name != "desk phone" {
		t.Errorf("the stored name is %q, want %q", peer.Name, "desk phone")
	}

	settings, err := newClient(t, two.Node, api.Options{DeviceName: "two"}).GetSettings(ctx,
		connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("read the settings of the peer: %v", err)
	}
	if settings.Msg.GetSettings().GetDeviceName() != "two" {
		t.Errorf("the name of the peer changed to %q", settings.Msg.GetSettings().GetDeviceName())
	}
	if two.Name() != two.name {
		t.Errorf("the peer renamed itself to %q", two.Name())
	}

	// The name stays when the peer announces its own one on a new session.
	if _, err := client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
		DeviceId: one.DeviceID().String(),
		Name:     "laptop",
	})); err != nil {
		t.Fatalf("rename this device: %v", err)
	}
	waitPeerOnline(t, one, two.DeviceID())
	waitPeerOnline(t, two, one.DeviceID())
	peer, err = one.Trust().Get(ctx, two.DeviceID())
	if err != nil {
		t.Fatalf("read the renamed device: %v", err)
	}
	if peer.Name != "desk phone" {
		t.Errorf("a new session replaced the local name with %q", peer.Name)
	}
}

// TestRenameDeviceSelf checks that the name of this device changes, is stored
// and travels to the peers in the handshake.
func TestRenameDeviceSelf(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 4700)
	two := newNode(t, network, bus, 4800)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)
	waitPeerOnline(t, one, two.DeviceID())
	waitPeerOnline(t, two, one.DeviceID())

	client := newClient(t, one.Node, api.Options{DeviceName: one.name})
	ctx := callTimeout(t)
	response, err := client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
		DeviceId: one.DeviceID().String(),
		Name:     "workstation",
	}))
	if err != nil {
		t.Fatalf("rename the device: %v", err)
	}
	if response.Msg.GetDevice().GetName() != "workstation" {
		t.Errorf("the response names the device %q", response.Msg.GetDevice().GetName())
	}
	if one.Name() != "workstation" {
		t.Errorf("the node runs as %q, want %q", one.Name(), "workstation")
	}

	status, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		t.Fatalf("get the status: %v", err)
	}
	if status.Msg.GetDeviceName() != "workstation" {
		t.Errorf("the status names the device %q", status.Msg.GetDeviceName())
	}
	settings, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("get the settings: %v", err)
	}
	if settings.Msg.GetSettings().GetDeviceName() != "workstation" {
		t.Errorf("the settings name the device %q", settings.Msg.GetSettings().GetDeviceName())
	}

	waitTrustName(t, two, one.DeviceID(), "workstation")
}

// TestRenameDeviceInvalid checks the refused names and the unknown device.
func TestRenameDeviceInvalid(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	one := newNode(t, network, bus, 4900)
	two := newNode(t, network, bus, 5000)
	one.start(t)
	two.start(t)
	one.pair(t, two)

	client := newClient(t, one.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)
	for _, name := range []string{"   ", strings.Repeat("a", 129)} {
		_, err := client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
			DeviceId: two.DeviceID().String(),
			Name:     name,
		}))
		requireCode(t, err, connect.CodeInvalidArgument)
	}
	_, err := client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
		DeviceId: "",
		Name:     "fine",
	}))
	requireCode(t, err, connect.CodeInvalidArgument)
	_, err = client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
		DeviceId: "not-a-device",
		Name:     "fine",
	}))
	requireCode(t, err, connect.CodeInvalidArgument)

	peer, err := one.Trust().Get(ctx, two.DeviceID())
	if err != nil {
		t.Fatalf("read the device: %v", err)
	}
	if peer.Name != two.name {
		t.Errorf("a refused rename stored the name %q", peer.Name)
	}

	unknown := model.DeviceID{7}
	_, err = client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
		DeviceId: unknown.String(),
		Name:     "x",
	}))
	requireCode(t, err, connect.CodeNotFound)
}

// TestListDevicesNotRunning checks that the device calls need a started node.
func TestListDevicesNotRunning(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 5100)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	_, err := client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	requireCode(t, err, connect.CodeFailedPrecondition)

	_, err = client.RenameDevice(ctx, connect.NewRequest(&localv1.RenameDeviceRequest{
		DeviceId: n.DeviceID().String(),
		Name:     "one",
	}))
	requireCode(t, err, connect.CodeFailedPrecondition)
}

// waitTrustName waits until the node stored the name of the peer.
func waitTrustName(t *testing.T, n *testNode, id model.DeviceID, name string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	var last string
	for time.Now().Before(deadline) {
		peer, err := n.Trust().Get(context.Background(), id)
		if err == nil {
			last = peer.Name
			if peer.Name == name {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the peer name is %q, want %q", last, name)
}
