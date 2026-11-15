package trust

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testRevocation signs a revocation of subject by issuer.
func testRevocation(t *testing.T, issuer, subject *identity.Identity, at time.Time) Revocation {
	t.Helper()

	revocation, err := NewRevocation(issuer, subject.DeviceID(), at)
	if err != nil {
		t.Fatalf("sign revocation: %v", err)
	}
	return revocation
}

func TestRevocationRoundTrip(t *testing.T) {
	issuer := testIdentity(t, 41)
	subject := testIdentity(t, 42)
	at := time.UnixMilli(1_760_000_000_000)

	revocation := testRevocation(t, issuer, subject, at)
	decoded, err := RevocationFromProto(revocation.Proto())
	if err != nil {
		t.Fatalf("decode revocation: %v", err)
	}
	if decoded.Subject != subject.DeviceID() || decoded.Issuer != issuer.DeviceID() {
		t.Fatalf("decoded revocation of %s by %s", decoded.Subject, decoded.Issuer)
	}
	if !decoded.IssuedAt.Equal(at) {
		t.Fatalf("decoded time %s, want %s", decoded.IssuedAt, at)
	}
	if err := decoded.Verify(issuer.Public()); err != nil {
		t.Fatalf("verify revocation: %v", err)
	}
}

func TestRevocationRejectsTampering(t *testing.T) {
	issuer := testIdentity(t, 41)
	subject := testIdentity(t, 42)
	other := testIdentity(t, 43)
	at := time.UnixMilli(1_760_000_000_000)

	cases := map[string]func(*wirev1.Revocation){
		"another subject": func(message *wirev1.Revocation) { message.Subject = other.DeviceID().Bytes() },
		"another issuer":  func(message *wirev1.Revocation) { message.Issuer = other.DeviceID().Bytes() },
		"another time":    func(message *wirev1.Revocation) { message.TsUnixMs = at.Add(time.Hour).UnixMilli() },
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			message := testRevocation(t, issuer, subject, at).Proto()
			tamper(message)
			revocation, err := RevocationFromProto(message)
			if err != nil {
				return
			}
			if err := revocation.Verify(issuer.Public()); err == nil {
				t.Fatal("a changed revocation still verifies")
			}
		})
	}
}

func TestApplyRevocationRevokesPeer(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	issuer := testIdentity(t, 41)
	subject := testIdentity(t, 42)
	addTestPeer(t, store, issuer, model.RolePersonal)
	addTestPeer(t, store, subject, model.RolePersonal)

	if err := store.UpdateAddrs(ctx, subject.DeviceID(), []string{"127.0.0.1:9000"}); err != nil {
		t.Fatalf("cache an address: %v", err)
	}
	intro := testIntroduction(t, issuer, subject, time.Now())
	if _, err := store.AddIntroduction(ctx, intro); err != nil {
		t.Fatalf("store an introduction: %v", err)
	}

	revocation := testRevocation(t, issuer, subject, time.Now())
	revoked, err := store.ApplyRevocation(ctx, local.DeviceID(), revocation)
	if err != nil {
		t.Fatalf("apply revocation: %v", err)
	}
	if !revoked {
		t.Fatal("the subject was not revoked")
	}

	peer, err := store.Get(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read the revoked peer: %v", err)
	}
	if !peer.Revoked {
		t.Fatal("the peer is not revoked")
	}
	if len(peer.LastAddrs) != 0 {
		t.Fatalf("the revoked peer keeps the addresses %v", peer.LastAddrs)
	}
	if _, err := store.IntroductionBySubject(ctx, subject.DeviceID()); !errors.Is(err, ErrIntroductionNotFound) {
		t.Fatalf("the introduction of the revoked peer: %v", err)
	}

	stored, err := store.RevocationBySubject(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read the stored revocation: %v", err)
	}
	if stored.Issuer != issuer.DeviceID() || stored.Subject != subject.DeviceID() {
		t.Fatalf("stored revocation %+v", stored)
	}
	heads, err := store.RevocationHeads(ctx)
	if err != nil {
		t.Fatalf("read the revocation heads: %v", err)
	}
	if len(heads) != 1 || heads[0].Subject != subject.DeviceID() {
		t.Fatalf("revocation heads %v", heads)
	}

	// A replay of the same record is refused, the deny-list stays.
	revoked, err = store.ApplyRevocation(ctx, local.DeviceID(), revocation)
	if err != nil {
		t.Fatalf("apply the same revocation again: %v", err)
	}
	if revoked {
		t.Fatal("a replayed revocation revoked the subject again")
	}
}

func TestApplyRevocationRefusesUntrustedIssuer(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	issuer := testIdentity(t, 41)
	subject := testIdentity(t, 42)
	friend := testIdentity(t, 43)
	addTestPeer(t, store, subject, model.RolePersonal)

	revocation := testRevocation(t, issuer, subject, time.Now())
	if _, err := store.ApplyRevocation(ctx, local.DeviceID(), revocation); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("a revocation of an unknown issuer: %v", err)
	}

	addTestPeer(t, store, friend, model.RoleFriends)
	revocation = testRevocation(t, friend, subject, time.Now())
	if _, err := store.ApplyRevocation(ctx, local.DeviceID(), revocation); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("a revocation of a friend: %v", err)
	}

	peer, err := store.Get(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read the subject: %v", err)
	}
	if peer.Revoked {
		t.Fatal("a refused revocation revoked the subject")
	}
}

func TestApplyRevocationRefusesRevokedIssuer(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	issuer := testIdentity(t, 41)
	other := testIdentity(t, 42)
	subject := testIdentity(t, 43)
	addTestPeer(t, store, issuer, model.RolePersonal)
	addTestPeer(t, store, other, model.RolePersonal)
	addTestPeer(t, store, subject, model.RolePersonal)

	if _, err := store.AddRevocation(ctx, testRevocation(t, other, issuer, time.Now())); err != nil {
		t.Fatalf("revoke the issuer: %v", err)
	}
	if _, err := store.ApplyRevocation(ctx, local.DeviceID(), testRevocation(t, issuer, subject, time.Now())); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("a revocation of a revoked issuer: %v", err)
	}
}

func TestApplyRevocationRefusesBrokenSignature(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	issuer := testIdentity(t, 41)
	subject := testIdentity(t, 42)
	addTestPeer(t, store, issuer, model.RolePersonal)
	addTestPeer(t, store, subject, model.RolePersonal)

	revocation := testRevocation(t, issuer, subject, time.Now())
	revocation.Signature.Ed25519[0] ^= 0xff
	revocation.Signature.MLDSA[0] ^= 0xff
	if _, err := store.ApplyRevocation(ctx, local.DeviceID(), revocation); err == nil {
		t.Fatal("a forged revocation was applied")
	}

	peer, err := store.Get(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read the subject: %v", err)
	}
	if peer.Revoked {
		t.Fatal("a forged revocation revoked the subject")
	}
}

func TestApplyRevocationRefusesUnknownSubject(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	issuer := testIdentity(t, 41)
	subject := testIdentity(t, 42)
	addTestPeer(t, store, issuer, model.RolePersonal)

	if _, err := store.ApplyRevocation(ctx, local.DeviceID(), testRevocation(t, issuer, subject, time.Now())); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("a revocation of an unknown device: %v", err)
	}
}

func TestApplyRevocationRefusesFarFuture(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	issuer := testIdentity(t, 41)
	subject := testIdentity(t, 42)
	addTestPeer(t, store, issuer, model.RolePersonal)
	addTestPeer(t, store, subject, model.RolePersonal)

	at := time.Now().Add(MaxClockSkew + time.Hour)
	if _, err := store.ApplyRevocation(ctx, local.DeviceID(), testRevocation(t, issuer, subject, at)); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("a revocation far in the future: %v", err)
	}
}

func TestApplyRevocationOfLocalDevice(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	issuer := testIdentity(t, 41)
	addTestPeer(t, store, issuer, model.RolePersonal)

	if _, err := store.ApplyRevocation(ctx, local.DeviceID(), testRevocation(t, issuer, local, time.Now())); !errors.Is(err, ErrSelfRevocation) {
		t.Fatalf("a revocation of the local device: %v", err)
	}
}

func TestApplySelfRevocation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	subject := testIdentity(t, 42)
	addTestPeer(t, store, subject, model.RolePersonal)

	revoked, err := store.ApplyRevocation(ctx, local.DeviceID(), testRevocation(t, subject, subject, time.Now()))
	if err != nil {
		t.Fatalf("apply a self revocation: %v", err)
	}
	if !revoked {
		t.Fatal("the self revocation did not revoke the device")
	}
	peer, err := store.Get(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read the revoked peer: %v", err)
	}
	if !peer.Revoked {
		t.Fatal("the peer is not revoked")
	}
}

func TestAddRevocation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	local := testIdentity(t, 40)
	subject := testIdentity(t, 42)
	addTestPeer(t, store, subject, model.RolePersonal)

	revocation := testRevocation(t, local, subject, time.Now())
	added, err := store.AddRevocation(ctx, revocation)
	if err != nil || !added {
		t.Fatalf("add a revocation: added %v, error %v", added, err)
	}
	peer, err := store.Get(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read the revoked peer: %v", err)
	}
	if !peer.Revoked {
		t.Fatal("the peer is not revoked")
	}

	// The deny-list refuses the device.
	err = store.Add(ctx, Peer{
		Public: subject.Public(),
		Name:   "again",
		Role:   model.RolePersonal,
	})
	if !errors.Is(err, ErrPeerRevoked) {
		t.Fatalf("adding a revoked device: %v", err)
	}
}

func TestRotateDiscoveryKey(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	first, err := store.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure the discovery key: %v", err)
	}
	keys, err := store.DiscoveryKeys(ctx)
	if err != nil {
		t.Fatalf("read the discovery keys: %v", err)
	}
	if len(keys) != 1 || !bytes.Equal(keys[0], first) {
		t.Fatalf("keys of a group without a rotation: %d", len(keys))
	}

	rotated, err := store.RotateDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("rotate the discovery key: %v", err)
	}
	if bytes.Equal(rotated, first) {
		t.Fatal("the rotated key is the old one")
	}
	current, err := store.DiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("read the current discovery key: %v", err)
	}
	if !bytes.Equal(current, rotated) {
		t.Fatal("the current key is not the rotated one")
	}

	keys, err = store.DiscoveryKeys(ctx)
	if err != nil {
		t.Fatalf("read the discovery keys: %v", err)
	}
	if len(keys) != 2 || !bytes.Equal(keys[0], rotated) || !bytes.Equal(keys[1], first) {
		t.Fatalf("keys after a rotation: %d", len(keys))
	}
}

func TestDiscoveryKeysDropPreviousAfterTransition(t *testing.T) {
	ctx := context.Background()
	trustStore, db := openTestStoreWithDB(t)

	first, err := trustStore.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure the discovery key: %v", err)
	}
	if _, err := trustStore.RotateDiscoveryKey(ctx); err != nil {
		t.Fatalf("rotate the discovery key: %v", err)
	}

	// The transition ends, so only the current key matches.
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		return store.NewSettings(tx, db.Sealer()).SetInt(ctx, discoveryPrevUntilKey, time.Now().Add(-time.Minute).UnixMilli())
	})
	if err != nil {
		t.Fatalf("end the transition: %v", err)
	}

	keys, err := trustStore.DiscoveryKeys(ctx)
	if err != nil {
		t.Fatalf("read the discovery keys: %v", err)
	}
	if len(keys) != 1 || bytes.Equal(keys[0], first) {
		t.Fatalf("keys after the transition: %d", len(keys))
	}
}

func TestApplyGroupDataKeepsPreviousKey(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	first, err := store.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure the discovery key: %v", err)
	}

	next := bytes.Repeat([]byte{9}, secretSize)
	at := time.Now().Add(time.Second).Truncate(time.Millisecond)
	changed, err := store.ApplyGroupData(ctx, GroupData{DiscoveryKey: next, DiscoveryAt: at})
	if err != nil || !changed {
		t.Fatalf("apply the group data: changed %v, error %v", changed, err)
	}

	keys, err := store.DiscoveryKeys(ctx)
	if err != nil {
		t.Fatalf("read the discovery keys: %v", err)
	}
	if len(keys) != 2 || !bytes.Equal(keys[0], next) || !bytes.Equal(keys[1], first) {
		t.Fatalf("keys after a group change: %d", len(keys))
	}
}

func TestLeaveGroup(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	personal := testIdentity(t, 41)
	friend := testIdentity(t, 42)
	addTestPeer(t, store, personal, model.RolePersonal)
	addTestPeer(t, store, friend, model.RoleFriends)
	if _, err := store.AddIntroduction(ctx, testIntroduction(t, personal, friend, time.Now())); err != nil {
		t.Fatalf("store an introduction: %v", err)
	}
	if err := store.SetRendezvousKey(ctx, personal.DeviceID(), bytes.Repeat([]byte{3}, secretSize)); err != nil {
		t.Fatalf("store a rendezvous key: %v", err)
	}
	if err := store.SetRendezvousKey(ctx, friend.DeviceID(), bytes.Repeat([]byte{4}, secretSize)); err != nil {
		t.Fatalf("store a friend rendezvous key: %v", err)
	}
	if err := store.SetRelaySettings(ctx, RelaySettings{URL: "https://relay.example", Token: "group"}); err != nil {
		t.Fatalf("store the relay settings: %v", err)
	}
	if _, err := store.EnsureDiscoveryKey(ctx); err != nil {
		t.Fatalf("ensure the discovery key: %v", err)
	}

	if err := store.LeaveGroup(ctx); err != nil {
		t.Fatalf("leave the group: %v", err)
	}

	peers, err := store.List(ctx, ListFilter{IncludeRevoked: true})
	if err != nil {
		t.Fatalf("list the peers: %v", err)
	}
	if len(peers) != 1 || peers[0].DeviceID() != friend.DeviceID() {
		t.Fatalf("peers after leaving the group: %v", peers)
	}
	if _, err := store.RendezvousKey(ctx, personal.DeviceID()); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("the rendezvous key of a group device: %v", err)
	}
	if _, err := store.RendezvousKey(ctx, friend.DeviceID()); err != nil {
		t.Fatalf("the rendezvous key of a friend: %v", err)
	}
	if _, err := store.DiscoveryKey(ctx); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("the discovery key of the group: %v", err)
	}
	data, err := store.GroupData(ctx)
	if err != nil {
		t.Fatalf("read the group data: %v", err)
	}
	if !data.Relay.Empty() || !data.DiscoveryAt.IsZero() {
		t.Fatalf("group data after leaving the group: %+v", data)
	}
}
