package api_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// TestPairingQr checks the code of the pairing window.
func TestPairingQr(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3000)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	// No window, so no code.
	_, err := client.GetPairingQr(ctx, connect.NewRequest(&localv1.GetPairingQrRequest{}))
	requireCode(t, err, connect.CodeFailedPrecondition)

	opened, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_PERSONAL,
	}))
	if err != nil {
		t.Fatalf("open pairing window: %v", err)
	}

	response, err := client.GetPairingQr(ctx, connect.NewRequest(&localv1.GetPairingQrRequest{}))
	if err != nil {
		t.Fatalf("get the pairing code: %v", err)
	}
	code := response.Msg.GetQr()
	if code.GetUri() == "" {
		t.Fatal("the code is empty")
	}
	if got, want := code.GetExpiresAtUnixMs(), opened.Msg.GetWindow().GetExpiresAtUnixMs(); got != want {
		t.Errorf("code deadline = %d, want %d", got, want)
	}

	parsed, err := pairing.ParseQR(code.GetUri())
	if err != nil {
		t.Fatalf("parse the code: %v", err)
	}
	if parsed.DeviceID != n.Identity().DeviceID() {
		t.Errorf("code device = %s, want %s", parsed.DeviceID, n.Identity().DeviceID())
	}
	if parsed.Fingerprint != n.Identity().CertFingerprint() {
		t.Errorf("code fingerprint = %s, want %s", parsed.Fingerprint, n.Identity().CertFingerprint())
	}
	if parsed.Role != model.RolePersonal {
		t.Errorf("code role = %s, want personal", parsed.Role)
	}
	if len(parsed.Addrs) != 1 || parsed.Addrs[0] != n.PairingAddr().String() {
		t.Errorf("code addresses = %v, want %s", parsed.Addrs, n.PairingAddr())
	}

	// A device that cannot list its interfaces passes the addresses itself.
	given, err := client.GetPairingQr(ctx, connect.NewRequest(&localv1.GetPairingQrRequest{
		Addrs: []string{"192.0.2.10:41234"},
	}))
	if err != nil {
		t.Fatalf("get the pairing code with addresses: %v", err)
	}
	givenCode, err := pairing.ParseQR(given.Msg.GetQr().GetUri())
	if err != nil {
		t.Fatalf("parse the code with addresses: %v", err)
	}
	if len(givenCode.Addrs) != 1 || givenCode.Addrs[0] != "192.0.2.10:41234" {
		t.Errorf("code addresses = %v, want the given one", givenCode.Addrs)
	}

	if _, err := client.ScanPairingQr(ctx, connect.NewRequest(&localv1.ScanPairingQrRequest{})); err == nil {
		t.Error("an empty code was accepted")
	} else {
		requireCode(t, err, connect.CodeInvalidArgument)
	}
	_, err = client.ScanPairingQr(ctx, connect.NewRequest(&localv1.ScanPairingQrRequest{
		Uri: "unison://pair?v=1&id=0&fp=0&t=0&a=0&role=none",
	}))
	requireCode(t, err, connect.CodeInvalidArgument)
}

// TestPairingQrScan checks a QR pairing between two nodes through the local API.
func TestPairingQrScan(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	showing := newNode(t, network, bus, 3100)
	scanner := newNode(t, network, bus, 3200)
	showing.start(t)
	scanner.start(t)

	showingClient := newClient(t, showing.Node, api.Options{DeviceName: "showing"})
	scannerClient := newClient(t, scanner.Node, api.Options{DeviceName: "scanner"})
	ctx := callTimeout(t)

	events := watch(t, showingClient)
	scannerEvents := watch(t, scannerClient)
	if _, err := showingClient.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_PERSONAL,
	})); err != nil {
		t.Fatalf("open pairing window: %v", err)
	}
	response, err := showingClient.GetPairingQr(ctx, connect.NewRequest(&localv1.GetPairingQrRequest{}))
	if err != nil {
		t.Fatalf("get the pairing code: %v", err)
	}

	if _, err := scannerClient.ScanPairingQr(ctx, connect.NewRequest(&localv1.ScanPairingQrRequest{
		Uri: response.Msg.GetQr().GetUri(),
	})); err != nil {
		t.Fatalf("scan the code: %v", err)
	}

	request := waitEvent(t, events, localv1.EventKind_EVENT_KIND_PAIRING_REQUEST)
	if request.GetPairingMethod() != localv1.PairingMethod_PAIRING_METHOD_QR {
		t.Errorf("pairing method = %s, want qr", request.GetPairingMethod())
	}
	if request.GetDeviceId() != scanner.DeviceID().String() {
		t.Errorf("request device = %s, want %s", request.GetDeviceId(), scanner.DeviceID())
	}
	if request.GetName() != "device-3200" {
		t.Errorf("request name = %q, want device-3200", request.GetName())
	}
	if request.GetSas().GetDigits() != "" {
		t.Errorf("a QR request shows the code %q", request.GetSas().GetDigits())
	}

	if _, err := showingClient.ConfirmSas(ctx, connect.NewRequest(&localv1.ConfirmSasRequest{
		DeviceId: scanner.DeviceID().String(),
	})); err != nil {
		t.Fatalf("confirm the request: %v", err)
	}
	paired := waitEvent(t, events, localv1.EventKind_EVENT_KIND_PAIRING_PAIRED)
	if paired.GetDeviceId() != scanner.DeviceID().String() {
		t.Errorf("paired device = %s, want %s", paired.GetDeviceId(), scanner.DeviceID())
	}
	scanned := waitEvent(t, scannerEvents, localv1.EventKind_EVENT_KIND_PAIRING_PAIRED)
	if scanned.GetDeviceId() != showing.DeviceID().String() {
		t.Errorf("scanned device = %s, want %s", scanned.GetDeviceId(), showing.DeviceID())
	}
	if scanned.GetPairingMethod() != localv1.PairingMethod_PAIRING_METHOD_QR {
		t.Errorf("scan pairing method = %s, want qr", scanned.GetPairingMethod())
	}

	peer, err := scanner.Trust().Get(ctx, showing.DeviceID())
	if err != nil {
		t.Fatalf("read the peer of the scanner: %v", err)
	}
	if peer.Role != model.RolePersonal {
		t.Errorf("scanner peer role = %s, want personal", peer.Role)
	}
	if _, err := showing.Trust().Get(ctx, scanner.DeviceID()); err != nil {
		t.Fatalf("read the peer of the showing device: %v", err)
	}
	waitOnline(t, scannerClient, showing.DeviceID())

	// The code of the window is spent and replaced.
	next, err := showingClient.GetPairingQr(ctx, connect.NewRequest(&localv1.GetPairingQrRequest{}))
	if err != nil {
		t.Fatalf("get the pairing code: %v", err)
	}
	if next.Msg.GetQr().GetUri() == response.Msg.GetQr().GetUri() {
		t.Error("the window kept the used code")
	}
}

// waitOnline waits until the status of the client reports the device as online.
func waitOnline(t *testing.T, client localv1connect.UnisonServiceClient, id model.DeviceID) {
	t.Helper()

	ctx := callTimeout(t)
	deadline := time.Now().Add(testTimeout)
	for {
		response, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
		if err != nil {
			t.Fatalf("get status: %v", err)
		}
		for _, device := range response.Msg.GetDevices() {
			if device.GetDeviceId() == id.String() && device.GetOnline() {
				return
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("device %s did not come online", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
