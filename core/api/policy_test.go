package api_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/mldsa"
	"testing"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestSetDeviceConfirmationSelf checks the local confirmation flag.
func TestSetDeviceConfirmationSelf(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 43000)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	if _, err := client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		Self:    true,
		Require: true,
	})); err != nil {
		t.Fatalf("set the local flag: %v", err)
	}
	settings, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if !settings.Msg.GetSettings().GetRequireConfirmation() {
		t.Fatal("the local confirmation flag is off")
	}

	if _, err := client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		Self:    true,
		Require: false,
	})); err != nil {
		t.Fatalf("clear the local flag: %v", err)
	}
	require, err := n.Trust().SelfRequireConfirmation(ctx)
	if err != nil {
		t.Fatalf("read the flag: %v", err)
	}
	if require {
		t.Fatal("the local confirmation flag is on")
	}

	_, err = client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		Self:     true,
		DeviceId: n.DeviceID().String(),
	}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{}))
	requireCode(t, err, connect.CodeInvalidArgument)
}

// TestSetDeviceConfirmationOfflinePeer checks the flag of a paired device that
// has no session.
func TestSetDeviceConfirmationOfflinePeer(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 44000)
	n.start(t)

	peer := offlinePeer(t, 3)
	if err := n.AddPeer(t.Context(), peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	_, err := client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		DeviceId: peer.DeviceID().String(),
		Require:  true,
	}))
	requireCode(t, err, connect.CodeUnavailable)

	_, err = client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		DeviceId: model.DeviceID{}.String(),
		Require:  true,
	}))
	requireCode(t, err, connect.CodeInvalidArgument)
}

// offlinePeer returns a Personal device that has no node behind it.
func offlinePeer(t *testing.T, seed byte) trust.Peer {
	t.Helper()

	edSeed := bytes.Repeat([]byte{seed}, ed25519.SeedSize)
	pqSeed := bytes.Repeat([]byte{seed, seed + 1}, mldsa.PrivateKeySize/2)
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	return trust.Peer{
		Public:   id.Public(),
		Name:     "offline",
		Platform: model.PlatformMacOS,
		Role:     model.RolePersonal,
	}
}
