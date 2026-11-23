package trust

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// policyPeers returns the applied policy target and the issuer of a policy test.
func policyPeers(t *testing.T) (local, issuer *identity.Identity) {
	t.Helper()

	return testIdentity(t, 9), testIdentity(t, 1)
}

func TestSetPeerPolicyRoundTrip(t *testing.T) {
	local, issuer := policyPeers(t)
	at := time.UnixMilli(1_700_000_000_000)

	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, true, at)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if err := policy.Verify(local.Public()); err == nil {
		t.Fatal("a policy verifies as another issuer")
	}
	if err := policy.Verify(issuer.Public()); err != nil {
		t.Fatalf("verify policy: %v", err)
	}

	decoded, err := SetPeerPolicyFromProto(policy.Proto())
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if decoded.Target != policy.Target || decoded.Scope != policy.Scope || decoded.RequireConfirmation != policy.RequireConfirmation {
		t.Fatal("the decoded policy differs")
	}
	if !decoded.SignedAt.Equal(policy.SignedAt) {
		t.Fatalf("signed at %v, want %v", decoded.SignedAt, policy.SignedAt)
	}
	if err := decoded.Verify(issuer.Public()); err != nil {
		t.Fatalf("verify the decoded policy: %v", err)
	}
}

func TestSetPeerPolicyFromProtoRejectsTampering(t *testing.T) {
	local, issuer := policyPeers(t)
	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, false, time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}

	message := policy.Proto()
	message.RequireConfirmation = true
	decoded, err := SetPeerPolicyFromProto(message)
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if err := decoded.Verify(issuer.Public()); err == nil {
		t.Fatal("a changed flag verifies")
	}

	message = policy.Proto()
	message.Scope = ScopeFiles.Proto()
	decoded, err = SetPeerPolicyFromProto(message)
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if err := decoded.Verify(issuer.Public()); err == nil {
		t.Fatal("a changed scope verifies")
	}

	message = policy.Proto()
	message.Scope = 0
	if _, err := SetPeerPolicyFromProto(message); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("policy without a scope error %v, want %v", err, ErrInvalidPeer)
	}

	if _, err := SetPeerPolicyFromProto(nil); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("empty policy error %v, want %v", err, ErrInvalidPeer)
	}
}

func TestApplySetPeerPolicy(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RolePersonal)

	at := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, true, at)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	changed, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy)
	if err != nil {
		t.Fatalf("apply policy: %v", err)
	}
	if !changed {
		t.Fatal("the flag did not change")
	}
	if got := peerConfirm(t, ts, issuer.DeviceID()); got != (Confirmations{Clipboard: true}) {
		t.Fatalf("flags for the issuer %+v, want clipboard only", got)
	}
	if self, err := ts.SelfConfirmations(ctx); err != nil || self.Any() {
		t.Fatalf("the policy changed the flags for every device: %+v, %v", self, err)
	}

	again, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, true, at.Add(time.Second))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	changed, err = ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), again)
	if err != nil {
		t.Fatalf("apply the newer policy: %v", err)
	}
	if changed {
		t.Fatal("a policy that repeats the flag reports a change")
	}
}

func TestApplySetPeerPolicyRejectsOld(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RolePersonal)

	at := time.Now().Add(-time.Minute)
	newest, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, true, at)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), newest); err != nil {
		t.Fatalf("apply policy: %v", err)
	}

	replay, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, false, at.Add(-time.Second))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), replay); !errors.Is(err, ErrPolicyReplay) {
		t.Fatalf("replay error %v, want %v", err, ErrPolicyReplay)
	}
	if got := peerConfirm(t, ts, issuer.DeviceID()); !got.Clipboard {
		t.Fatal("the replay changed the flag")
	}
}

func TestApplySetPeerPolicyRejectsOtherTarget(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RolePersonal)

	policy, err := NewSetPeerPolicy(issuer, testIdentity(t, 4).DeviceID(), ScopeClipboard, true, time.Now())
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrForeignPolicy) {
		t.Fatalf("foreign target error %v, want %v", err, ErrForeignPolicy)
	}
}

func TestApplySetPeerPolicyRejectsFriendsIssuer(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RoleFriends)

	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, true, time.Now())
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("friend issuer error %v, want %v", err, ErrInvalidPeer)
	}
	if got := peerConfirm(t, ts, issuer.DeviceID()); got.Any() {
		t.Fatalf("a friend changed the flag: %+v", got)
	}
}

func TestApplySetPeerPolicyRejectsUnknownIssuer(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)

	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeClipboard, true, time.Now())
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("unknown issuer error %v, want %v", err, ErrInvalidPeer)
	}
}

func TestApplySetPeerPolicyScopesAndIssuers(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	other := testIdentity(t, 5)
	addTestPeer(t, ts, issuer, model.RolePersonal)
	addTestPeer(t, ts, other, model.RolePersonal)

	at := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	files, err := NewSetPeerPolicy(issuer, local.DeviceID(), ScopeFiles, true, at)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), files); err != nil {
		t.Fatalf("apply the files policy: %v", err)
	}
	if got := peerConfirm(t, ts, issuer.DeviceID()); got != (Confirmations{Files: true}) {
		t.Fatalf("flags for the issuer %+v, want files only", got)
	}
	if got := peerConfirm(t, ts, other.DeviceID()); got.Any() {
		t.Fatalf("the policy changed the flags of another device: %+v", got)
	}

	// Each issuer has a clock of its own, so an older time of another issuer
	// is not a replay.
	older, err := NewSetPeerPolicy(other, local.DeviceID(), ScopeClipboard, true, at.Add(-time.Second))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), other.DeviceID(), older); err != nil {
		t.Fatalf("apply the policy of another issuer: %v", err)
	}
	if got := peerConfirm(t, ts, other.DeviceID()); got != (Confirmations{Clipboard: true}) {
		t.Fatalf("flags for the other issuer %+v, want clipboard only", got)
	}
}

// peerConfirm returns the flags of the store for a peer.
func peerConfirm(t *testing.T, ts *Store, id model.DeviceID) Confirmations {
	t.Helper()

	peer, err := ts.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	return peer.Confirm
}
