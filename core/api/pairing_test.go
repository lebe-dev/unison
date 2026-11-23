package api_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestPairingWindow checks the lifecycle of the pairing window.
func TestPairingWindow(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2500)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	opened, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_PERSONAL,
	}))
	if err != nil {
		t.Fatalf("open pairing window: %v", err)
	}
	window := opened.Msg.GetWindow()
	if !window.GetOpen() {
		t.Fatal("the pairing window is closed")
	}
	if window.GetAddr() == "" {
		t.Error("the pairing window has no address")
	}
	if window.GetExpiresAtUnixMs() == 0 {
		t.Error("the pairing window has no deadline")
	}

	_, err = client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_PERSONAL,
	}))
	requireCode(t, err, connect.CodeFailedPrecondition)

	candidates, err := client.ListPairingCandidates(ctx, connect.NewRequest(&localv1.ListPairingCandidatesRequest{}))
	if err != nil {
		t.Fatalf("list pairing candidates: %v", err)
	}
	if len(candidates.Msg.GetCandidates()) != 0 {
		t.Errorf("the window has %d candidates, want none", len(candidates.Msg.GetCandidates()))
	}

	extended, err := client.ExtendPairingWindow(ctx, connect.NewRequest(&localv1.ExtendPairingWindowRequest{}))
	if err != nil {
		t.Fatalf("extend pairing window: %v", err)
	}
	if !extended.Msg.GetWindow().GetOpen() {
		t.Error("the pairing window closed after the extension")
	}

	if _, err := client.ClosePairingWindow(ctx, connect.NewRequest(&localv1.ClosePairingWindowRequest{})); err != nil {
		t.Fatalf("close pairing window: %v", err)
	}

	_, err = client.ExtendPairingWindow(ctx, connect.NewRequest(&localv1.ExtendPairingWindowRequest{}))
	requireCode(t, err, connect.CodeFailedPrecondition)
}

// TestOpenPairingWindowRole checks the proposed role.
func TestOpenPairingWindowRole(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2600)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	_, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{}))
	requireCode(t, err, connect.CodeInvalidArgument)

	if _, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_FRIENDS,
	})); err != nil {
		t.Fatalf("open pairing window: %v", err)
	}
	if err := n.ClosePairing(); err != nil {
		t.Fatalf("close pairing window: %v", err)
	}
}

// TestPairingWindowNotRunning checks the window of a stopped node.
func TestPairingWindowNotRunning(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2700)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	_, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_PERSONAL,
	}))
	requireCode(t, err, connect.CodeFailedPrecondition)
}

// TestConfirmSas checks the decisions of a closed window and of an unknown
// device.
func TestConfirmSas(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2800)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	id := model.DeviceID{1}.String()

	_, err := client.ConfirmSas(ctx, connect.NewRequest(&localv1.ConfirmSasRequest{DeviceId: id}))
	requireCode(t, err, connect.CodeFailedPrecondition)

	if _, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_PERSONAL,
	})); err != nil {
		t.Fatalf("open pairing window: %v", err)
	}

	_, err = client.ConfirmSas(ctx, connect.NewRequest(&localv1.ConfirmSasRequest{DeviceId: id}))
	requireCode(t, err, connect.CodeNotFound)

	_, err = client.RejectSas(ctx, connect.NewRequest(&localv1.RejectSasRequest{DeviceId: id}))
	requireCode(t, err, connect.CodeNotFound)

	_, err = client.ConfirmSas(ctx, connect.NewRequest(&localv1.ConfirmSasRequest{DeviceId: "no-hex"}))
	requireCode(t, err, connect.CodeInvalidArgument)
}

// TestStartSas checks the refused pairing addresses.
func TestStartSas(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2900)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	if _, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{
		Role: localv1.Role_ROLE_PERSONAL,
	})); err != nil {
		t.Fatalf("open pairing window: %v", err)
	}

	_, err := client.StartSas(ctx, connect.NewRequest(&localv1.StartSasRequest{}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.StartSas(ctx, connect.NewRequest(&localv1.StartSasRequest{Addr: "localhost"}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.StartSas(ctx, connect.NewRequest(&localv1.StartSasRequest{Addr: strings.Repeat("a", 256)}))
	requireCode(t, err, connect.CodeInvalidArgument)
}
