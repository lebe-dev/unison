package trust

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

// TestAllowRemoteSettings checks the permission of every peer, which is granted
// until the user turns it off.
func TestAllowRemoteSettings(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	allowed, err := ts.AllowRemoteSettings(ctx)
	if err != nil {
		t.Fatalf("read the permission: %v", err)
	}
	if !allowed {
		t.Fatal("every device is denied by default")
	}

	if err := ts.SetAllowRemoteSettings(ctx, false); err != nil {
		t.Fatalf("deny every device: %v", err)
	}
	allowed, err = ts.AllowRemoteSettings(ctx)
	if err != nil {
		t.Fatalf("read the permission: %v", err)
	}
	if allowed {
		t.Fatal("the permission is still granted")
	}
}

// TestSetPeerAllowRemoteSettings checks the permission of one peer and that it
// leaves the other flags alone.
func TestSetPeerAllowRemoteSettings(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 21, model.RolePersonal)
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	id := peer.DeviceID()

	stored, err := ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if stored.RemoteSettingsDenied {
		t.Fatal("the permission is denied by default")
	}

	changed, err := ts.SetPeerAllowRemoteSettings(ctx, id, false)
	if err != nil || !changed {
		t.Fatalf("deny the peer: changed %t, %v", changed, err)
	}
	if changed, err := ts.SetPeerAllowRemoteSettings(ctx, id, false); err != nil || changed {
		t.Fatalf("deny the peer again: changed %t, %v", changed, err)
	}
	stored, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if !stored.RemoteSettingsDenied {
		t.Fatal("the permission is still granted")
	}

	changed, err = ts.SetPeerAllowRemoteSettings(ctx, id, true)
	if err != nil || !changed {
		t.Fatalf("allow the peer: changed %t, %v", changed, err)
	}
	stored, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if stored.RemoteSettingsDenied {
		t.Fatal("the permission is still denied")
	}

	other := testPeer(t, 22, model.RolePersonal).DeviceID()
	if _, err := ts.SetPeerAllowRemoteSettings(ctx, other, false); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("set the permission of an unknown peer: got %v, want ErrPeerNotFound", err)
	}
}

// TestApplySetPeerPolicyRejectsDeniedPeer checks that a peer that may not change
// the settings of this device cannot change a confirmation flag.
func TestApplySetPeerPolicyRejectsDeniedPeer(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RolePersonal)

	if _, err := ts.SetPeerAllowRemoteSettings(ctx, issuer.DeviceID(), false); err != nil {
		t.Fatalf("deny the peer: %v", err)
	}
	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, true, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrRemoteSettingsDenied) {
		t.Fatalf("apply the policy: got %v, want %v", err, ErrRemoteSettingsDenied)
	}
	if got := peerConfirm(t, ts, issuer.DeviceID()); got.Any() {
		t.Fatalf("the denied peer changed the flag: %+v", got)
	}

	if _, err := ts.SetPeerAllowRemoteSettings(ctx, issuer.DeviceID(), true); err != nil {
		t.Fatalf("allow the peer: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); err != nil {
		t.Fatalf("apply the policy after the permission: %v", err)
	}
	if got := peerConfirm(t, ts, issuer.DeviceID()); !got.Clipboard {
		t.Fatalf("the allowed peer did not change the flag: %+v", got)
	}
}

// TestApplySetPeerPolicyRejectsDeniedEverywhere checks the switch that covers
// every paired device.
func TestApplySetPeerPolicyRejectsDeniedEverywhere(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RolePersonal)

	if err := ts.SetAllowRemoteSettings(ctx, false); err != nil {
		t.Fatalf("deny every device: %v", err)
	}
	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeFiles, true, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrRemoteSettingsDenied) {
		t.Fatalf("apply the policy: got %v, want %v", err, ErrRemoteSettingsDenied)
	}
	if got := peerConfirm(t, ts, issuer.DeviceID()); got.Any() {
		t.Fatalf("the peer changed the flag: %+v", got)
	}
}
