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

	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), true, at)
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
	if decoded.Target != policy.Target || decoded.RequireConfirmation != policy.RequireConfirmation {
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
	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), false, time.UnixMilli(1_700_000_000_000))
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
	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), true, at)
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
	require, err := ts.SelfRequireConfirmation(ctx)
	if err != nil {
		t.Fatalf("read the flag: %v", err)
	}
	if !require {
		t.Fatal("the confirmation flag is off")
	}

	again, err := NewSetPeerPolicy(issuer, local.DeviceID(), true, at.Add(time.Second))
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
	newest, err := NewSetPeerPolicy(issuer, local.DeviceID(), true, at)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), newest); err != nil {
		t.Fatalf("apply policy: %v", err)
	}

	replay, err := NewSetPeerPolicy(issuer, local.DeviceID(), false, at.Add(-time.Second))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), replay); !errors.Is(err, ErrPolicyReplay) {
		t.Fatalf("replay error %v, want %v", err, ErrPolicyReplay)
	}
	if require, err := ts.SelfRequireConfirmation(ctx); err != nil || !require {
		t.Fatalf("the replay changed the flag: %t, %v", require, err)
	}
}

func TestApplySetPeerPolicyRejectsOtherTarget(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RolePersonal)

	policy, err := NewSetPeerPolicy(issuer, testIdentity(t, 4).DeviceID(), true, time.Now())
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

	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), true, time.Now())
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("friend issuer error %v, want %v", err, ErrInvalidPeer)
	}
	if require, err := ts.SelfRequireConfirmation(ctx); err != nil || require {
		t.Fatalf("a friend changed the flag: %t, %v", require, err)
	}
}

func TestApplySetPeerPolicyRejectsUnknownIssuer(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)

	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), true, time.Now())
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("unknown issuer error %v, want %v", err, ErrInvalidPeer)
	}
}

func TestSetSelfRequireConfirmationDatesTheChoice(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	local, issuer := policyPeers(t)
	addTestPeer(t, ts, issuer, model.RolePersonal)

	policy, err := NewSetPeerPolicy(issuer, local.DeviceID(), true, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if err := ts.SetSelfRequireConfirmation(ctx, false); err != nil {
		t.Fatalf("set the flag: %v", err)
	}
	if _, err := ts.ApplySetPeerPolicy(ctx, local.DeviceID(), issuer.DeviceID(), policy); !errors.Is(err, ErrPolicyReplay) {
		t.Fatalf("policy older than the local choice error %v, want %v", err, ErrPolicyReplay)
	}
}
