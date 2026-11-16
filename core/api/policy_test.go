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
	"github.com/lebe-dev/unison/core/policy"
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
		Scope:   localv1.ConfirmationScope_CONFIRMATION_SCOPE_CLIPBOARD,
	})); err != nil {
		t.Fatalf("set the local flag: %v", err)
	}
	if _, err := client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		Self:    true,
		Require: true,
		Scope:   localv1.ConfirmationScope_CONFIRMATION_SCOPE_FILES,
	})); err != nil {
		t.Fatalf("set the local files flag: %v", err)
	}
	settings, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if !settings.Msg.GetSettings().GetRequireConfirmation() {
		t.Fatal("the local confirmation flag is off")
	}
	if !settings.Msg.GetSettings().GetRequireFileConfirmation() {
		t.Fatal("the local files flag is off")
	}

	if _, err := client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		Self:    true,
		Require: false,
		Scope:   localv1.ConfirmationScope_CONFIRMATION_SCOPE_CLIPBOARD,
	})); err != nil {
		t.Fatalf("clear the local flag: %v", err)
	}
	require, err := n.Trust().SelfRequireConfirmation(ctx, trust.ScopeClipboard)
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
		Scope:    localv1.ConfirmationScope_CONFIRMATION_SCOPE_FILES,
		Side:     localv1.ConfirmationSide_CONFIRMATION_SIDE_PEER,
	}))
	requireCode(t, err, connect.CodeUnavailable)

	// The flag of this device needs no session with the device.
	if _, err := client.SetDeviceConfirmation(ctx, connect.NewRequest(&localv1.SetDeviceConfirmationRequest{
		DeviceId: peer.DeviceID().String(),
		Require:  true,
		Scope:    localv1.ConfirmationScope_CONFIRMATION_SCOPE_FILES,
		Side:     localv1.ConfirmationSide_CONFIRMATION_SIDE_LOCAL,
	})); err != nil {
		t.Fatalf("set the local flag of an offline device: %v", err)
	}

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

// TestUpdatePolicyRoundTrip checks that the policy table survives the API and
// that a partial change keeps the other cells.
func TestUpdatePolicyRoundTrip(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2500)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	action := localv1.PolicyAction_POLICY_ACTION_CONFIRM
	size := int64(2 << 20)
	response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		Metered: &localv1.PolicyRow{Image: &localv1.PolicyCell{Action: &action, MaxSizeBytes: &size}},
	}))
	if err != nil {
		t.Fatalf("update the policy: %v", err)
	}

	settings := response.Msg.GetSettings()
	image := settings.GetMetered().GetImage()
	if image.GetAction() != action {
		t.Errorf("metered image action is %s, want %s", image.GetAction(), action)
	}
	if image.GetMaxSizeBytes() != size {
		t.Errorf("metered image limit is %d, want %d", image.GetMaxSizeBytes(), size)
	}
	if !settings.GetMetered().GetRelay() {
		t.Error("the partial change turns the relay of the metered row off")
	}
	if got := settings.GetMetered().GetText().GetAction(); got != localv1.PolicyAction_POLICY_ACTION_AUTO {
		t.Errorf("metered text action is %s, want auto", got)
	}
	if got := settings.GetUnmetered().GetImage().GetMaxSizeBytes(); got != policy.DefaultLANMaxSize {
		t.Errorf("unmetered image limit is %d, want %d", got, policy.DefaultLANMaxSize)
	}

	stored, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if got := stored.Msg.GetSettings().GetMetered().GetImage().GetAction(); got != action {
		t.Errorf("stored metered image action is %s, want %s", got, action)
	}
}

// TestUpdatePolicyChangesDecision checks that a new threshold applies to the
// next item without a restart.
func TestUpdatePolicyChangesDecision(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	one := newNode(t, network, bus, 2600)
	two := newNode(t, network, bus, 2610)
	one.start(t)
	two.start(t)
	one.pair(t, two)
	two.pair(t, one)
	waitPeerOnline(t, one, two.DeviceID())
	waitPeerOnline(t, two, one.DeviceID())

	client := newClient(t, one.Node, api.Options{DeviceName: one.name})
	ctx := callTimeout(t)

	item := model.ClipItem{
		Kind:            model.KindText,
		Representations: []model.Representation{{MIME: "text/plain", Size: 2 << 20}},
	}
	result, err := one.Policy().DecideItem(ctx, two.DeviceID(), item, model.SourceClipboard)
	if err != nil {
		t.Fatalf("decide the item: %v", err)
	}
	if result.Action != policy.ActionAutoApply {
		t.Fatalf("a 2 MiB text item is %s, want auto", result.Action)
	}

	size := int64(1 << 20)
	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		Unmetered: &localv1.PolicyRow{Text: &localv1.PolicyCell{MaxSizeBytes: &size}},
	})); err != nil {
		t.Fatalf("update the policy: %v", err)
	}

	result, err = one.Policy().DecideItem(ctx, two.DeviceID(), item, model.SourceClipboard)
	if err != nil {
		t.Fatalf("decide the item again: %v", err)
	}
	if result.Action != policy.ActionMetadataOnly {
		t.Fatalf("the 2 MiB text item is %s after the change, want metadata", result.Action)
	}
}

// TestUpdatePolicyReset checks that the reset removes the stored overrides.
func TestUpdatePolicyReset(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2700)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	action := localv1.PolicyAction_POLICY_ACTION_REJECT
	size := int64(1 << 20)
	relay := false
	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		Roaming:           &localv1.PolicyRow{Text: &localv1.PolicyCell{Action: &action}, Relay: &relay},
		RelayMaxSizeBytes: &size,
	})); err != nil {
		t.Fatalf("update the policy: %v", err)
	}

	response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{ResetPolicy: true}))
	if err != nil {
		t.Fatalf("reset the policy: %v", err)
	}

	settings := response.Msg.GetSettings()
	text := settings.GetRoaming().GetText()
	if text.GetAction() != localv1.PolicyAction_POLICY_ACTION_AUTO {
		t.Errorf("roaming text action is %s after the reset, want auto", text.GetAction())
	}
	if text.GetMaxSizeBytes() != policy.DefaultRelayMaxSize {
		t.Errorf("roaming text limit is %d after the reset, want %d", text.GetMaxSizeBytes(), policy.DefaultRelayMaxSize)
	}
	if settings.GetRoaming().GetRelay() {
		t.Error("the relay of the roaming row stays off after the reset")
	}
	if settings.GetRelayMaxSizeBytes() != policy.DefaultRelayMaxSize {
		t.Errorf("relay limit is %d after the reset, want %d",
			settings.GetRelayMaxSizeBytes(), policy.DefaultRelayMaxSize)
	}

	stored, err := n.Store().Settings().GetString(ctx, policy.ModeKey(policy.ConditionRoaming, model.KindText), "")
	if err != nil {
		t.Fatalf("read the stored mode: %v", err)
	}
	if stored != "" {
		t.Errorf("the stored mode is %q after the reset", stored)
	}
}

// TestUpdatePolicyInvalid checks the refused policy values.
func TestUpdatePolicyInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2800)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	negative := int64(-1)
	zero := int64(0)
	auto := localv1.PolicyAction_POLICY_ACTION_AUTO
	unspecified := localv1.PolicyAction_POLICY_ACTION_UNSPECIFIED
	cases := map[string]*localv1.UpdateSettingsRequest{
		"negative limit": {
			Unmetered: &localv1.PolicyRow{Text: &localv1.PolicyCell{MaxSizeBytes: &negative}},
		},
		"auto without a limit": {
			Roaming: &localv1.PolicyRow{Image: &localv1.PolicyCell{Action: &auto, MaxSizeBytes: &zero}},
		},
		"unspecified action": {
			Metered: &localv1.PolicyRow{Files: &localv1.PolicyCell{Action: &unspecified}},
		},
		"zero relay limit": {RelayMaxSizeBytes: &zero},
	}
	for name, request := range cases {
		_, err := client.UpdateSettings(ctx, connect.NewRequest(request))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: error is %v, want invalid argument", name, err)
		}
	}

	settings, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if got := settings.Msg.GetSettings().GetMetered().GetFiles().GetAction(); got != localv1.PolicyAction_POLICY_ACTION_METADATA {
		t.Errorf("metered files action is %s after the refused changes, want metadata", got)
	}
}
