package trust

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testIdentity returns a deterministic identity derived from seed.
func testIdentity(t *testing.T, seed byte) *identity.Identity {
	t.Helper()

	edSeed := bytes.Repeat([]byte{seed}, ed25519.SeedSize)
	pqSeed := bytes.Repeat([]byte{seed, seed + 1}, mldsa.PrivateKeySize/2)
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("create identity %d: %v", seed, err)
	}
	return id
}

// addTestPeer stores the identity of peer with the role.
func addTestPeer(t *testing.T, store *Store, peer *identity.Identity, role model.Role) {
	t.Helper()

	err := store.Add(context.Background(), Peer{
		Public: peer.Public(),
		Name:   fmt.Sprintf("device-%s", peer.DeviceID()),
		Role:   role,
	})
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
}

// testIntroduction signs an introduction of subject by issuer.
func testIntroduction(t *testing.T, issuer, subject *identity.Identity, at time.Time) Introduction {
	t.Helper()

	return testIntroductionNamed(t, issuer, subject, at, "subject")
}

// testIntroductionNamed signs an introduction with the given display name.
func testIntroductionNamed(t *testing.T, issuer, subject *identity.Identity, at time.Time, name string) Introduction {
	t.Helper()

	intro, err := NewIntroduction(issuer, IntroductionInfo{
		Subject:  subject.Public(),
		Name:     name,
		Platform: model.PlatformMacOS,
	}, at)
	if err != nil {
		t.Fatalf("sign introduction: %v", err)
	}
	return intro
}

func TestIntroductionRoundTrip(t *testing.T) {
	issuer := testIdentity(t, 1)
	subject := testIdentity(t, 2)
	at := time.UnixMilli(1_760_000_000_000)

	intro := testIntroduction(t, issuer, subject, at)
	decoded, err := IntroductionFromProto(intro.Proto())
	if err != nil {
		t.Fatalf("decode introduction: %v", err)
	}
	if decoded.DeviceID() != subject.DeviceID() || decoded.Issuer != issuer.DeviceID() {
		t.Fatalf("decoded introduction of %s by %s", decoded.DeviceID(), decoded.Issuer)
	}
	if !decoded.IssuedAt.Equal(at) || decoded.SubjectName != "subject" || decoded.Platform != model.PlatformMacOS {
		t.Fatalf("decoded introduction lost data: %+v", decoded)
	}
	if err := decoded.Verify(issuer.Public()); err != nil {
		t.Fatalf("verify introduction: %v", err)
	}
}

func TestIntroductionRejectsTampering(t *testing.T) {
	issuer := testIdentity(t, 1)
	subject := testIdentity(t, 2)
	other := testIdentity(t, 3)
	at := time.UnixMilli(1_760_000_000_000)

	cases := map[string]func(*introductionWire){
		"another subject": func(message *introductionWire) {
			message.SubjectEd25519 = other.Public().Ed25519
			message.SubjectMldsa = other.Public().MLDSABytes()
		},
		"another issuer": func(message *introductionWire) {
			message.Issuer = other.DeviceID().Bytes()
		},
		"another time": func(message *introductionWire) {
			message.TsUnixMs = at.Add(time.Hour).UnixMilli()
		},
		"another name": func(message *introductionWire) {
			message.SubjectName = "someone else"
		},
		"another platform": func(message *introductionWire) {
			message.Platform = wirev1.Platform_PLATFORM_ANDROID
		},
		"another role": func(message *introductionWire) {
			message.Role = wirev1.Role_ROLE_FRIENDS
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			message := testIntroduction(t, issuer, subject, at).Proto()
			tamper(message)
			intro, err := IntroductionFromProto(message)
			if err != nil {
				return
			}
			if err := intro.Verify(issuer.Public()); err == nil {
				t.Fatal("a changed introduction still verifies")
			}
		})
	}
}

// introductionWire is the wire form of an introduction, named to keep the table
// of the tampering test readable.
type introductionWire = wirev1.Introduction

func TestApplyIntroductionAddsPeer(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	issuer := testIdentity(t, 1)
	subject := testIdentity(t, 2)
	addTestPeer(t, store, issuer, model.RolePersonal)

	added, err := store.ApplyIntroduction(ctx, testIntroduction(t, issuer, subject, time.Now()))
	if err != nil {
		t.Fatalf("apply introduction: %v", err)
	}
	if !added {
		t.Fatal("the introduction did not add the device")
	}

	peer, err := store.Get(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read introduced peer: %v", err)
	}
	if peer.Role != model.RolePersonal {
		t.Fatalf("the introduced peer has role %s", peer.Role)
	}
	if peer.IntroducedBy != issuer.DeviceID() {
		t.Fatalf("the introduced peer came from %s", peer.IntroducedBy)
	}
	if peer.Name != "subject" || peer.Platform != model.PlatformMacOS {
		t.Fatalf("the introduced peer has no display data: %+v", peer)
	}

	heads, err := store.IntroductionHeads(ctx)
	if err != nil {
		t.Fatalf("read heads: %v", err)
	}
	if len(heads) != 1 || heads[0].Subject != subject.DeviceID() {
		t.Fatalf("stored heads %+v", heads)
	}
	stored, err := store.IntroductionBySubject(ctx, subject.DeviceID())
	if err != nil {
		t.Fatalf("read introduction: %v", err)
	}
	if err := stored.Verify(issuer.Public()); err != nil {
		t.Fatalf("stored introduction does not verify: %v", err)
	}
}

func TestApplyIntroductionRefuses(t *testing.T) {
	ctx := context.Background()
	at := time.UnixMilli(1_760_000_000_000)

	cases := []struct {
		name  string
		setup func(t *testing.T, store *Store, issuer *identity.Identity, subject *identity.Identity) Introduction
	}{
		{
			name: "unknown issuer",
			setup: func(t *testing.T, _ *Store, issuer, subject *identity.Identity) Introduction {
				return testIntroduction(t, issuer, subject, at)
			},
		},
		{
			name: "friends issuer",
			setup: func(t *testing.T, store *Store, issuer, subject *identity.Identity) Introduction {
				addTestPeer(t, store, issuer, model.RoleFriends)
				return testIntroduction(t, issuer, subject, at)
			},
		},
		{
			name: "revoked issuer",
			setup: func(t *testing.T, store *Store, issuer, subject *identity.Identity) Introduction {
				addTestPeer(t, store, issuer, model.RolePersonal)
				if err := store.MarkRevoked(ctx, issuer.DeviceID()); err != nil {
					t.Fatalf("revoke issuer: %v", err)
				}
				return testIntroduction(t, issuer, subject, at)
			},
		},
		{
			name: "broken signature",
			setup: func(t *testing.T, store *Store, issuer, subject *identity.Identity) Introduction {
				addTestPeer(t, store, issuer, model.RolePersonal)
				intro := testIntroduction(t, issuer, subject, at)
				intro.Signature.Ed25519[0] ^= 0xff
				return intro
			},
		},
		{
			name: "signature of another device",
			setup: func(t *testing.T, store *Store, issuer, subject *identity.Identity) Introduction {
				addTestPeer(t, store, issuer, model.RolePersonal)
				return testIntroduction(t, testIdentity(t, 9), subject, at)
			},
		},
		{
			name: "revoked subject",
			setup: func(t *testing.T, store *Store, issuer, subject *identity.Identity) Introduction {
				addTestPeer(t, store, issuer, model.RolePersonal)
				addTestPeer(t, store, subject, model.RolePersonal)
				if err := store.MarkRevoked(ctx, subject.DeviceID()); err != nil {
					t.Fatalf("revoke subject: %v", err)
				}
				return testIntroduction(t, issuer, subject, at)
			},
		},
		{
			name: "friends subject",
			setup: func(t *testing.T, store *Store, issuer, subject *identity.Identity) Introduction {
				addTestPeer(t, store, issuer, model.RolePersonal)
				addTestPeer(t, store, subject, model.RoleFriends)
				return testIntroduction(t, issuer, subject, at)
			},
		},
		{
			name: "friends role",
			setup: func(t *testing.T, store *Store, issuer, subject *identity.Identity) Introduction {
				addTestPeer(t, store, issuer, model.RolePersonal)
				intro := testIntroduction(t, issuer, subject, at)
				intro.Role = model.RoleFriends
				return intro
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := openTestStore(t)
			issuer := testIdentity(t, 1)
			subject := testIdentity(t, 2)
			intro := testCase.setup(t, store, issuer, subject)

			added, err := store.ApplyIntroduction(ctx, intro)
			if err == nil {
				t.Fatal("the introduction was applied")
			}
			if added {
				t.Fatal("the refused introduction added a peer")
			}
		})
	}
}

func TestApplyIntroductionIgnoresReplay(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	issuer := testIdentity(t, 1)
	subject := testIdentity(t, 2)
	addTestPeer(t, store, issuer, model.RolePersonal)

	at := time.UnixMilli(1_760_000_000_000)
	newer := testIntroduction(t, issuer, subject, at)
	if _, err := store.ApplyIntroduction(ctx, newer); err != nil {
		t.Fatalf("apply introduction: %v", err)
	}

	older := testIntroductionNamed(t, issuer, subject, at.Add(-time.Hour), "old name")
	if added, err := store.ApplyIntroduction(ctx, older); err != nil || added {
		t.Fatalf("an older introduction was applied: added %v, error %v", added, err)
	}
	if stored, err := store.IntroductionBySubject(ctx, subject.DeviceID()); err != nil || stored.SubjectName != "subject" {
		t.Fatalf("the stored record changed: %q, error %v", stored.SubjectName, err)
	}

	stale := testIntroduction(t, issuer, subject, at)
	if added, err := store.ApplyIntroduction(ctx, stale); err != nil || added {
		t.Fatalf("an equal introduction was applied: added %v, error %v", added, err)
	}

	latest := testIntroductionNamed(t, issuer, subject, at.Add(time.Hour), "new name")
	if _, err := store.ApplyIntroduction(ctx, latest); err != nil {
		t.Fatalf("apply newer introduction: %v", err)
	}
	if stored, err := store.IntroductionBySubject(ctx, subject.DeviceID()); err != nil || stored.SubjectName != "new name" {
		t.Fatalf("the newer record was not stored: %q, error %v", stored.SubjectName, err)
	}
}

func TestApplyIntroductionRefusesFarFutureTime(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	issuer := testIdentity(t, 1)
	subject := testIdentity(t, 2)
	addTestPeer(t, store, issuer, model.RolePersonal)

	ahead := testIntroduction(t, issuer, subject, time.Now().Add(MaxClockSkew+time.Hour))
	if _, err := store.ApplyIntroduction(ctx, ahead); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("an introduction far in the future: %v", err)
	}
	if _, err := store.Get(ctx, subject.DeviceID()); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("the refused introduction added a peer: %v", err)
	}
}

func TestAddIntroductionKeepsLocalRecord(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	issuer := testIdentity(t, 1)
	subject := testIdentity(t, 2)
	intro := testIntroduction(t, issuer, subject, time.Now())

	stored, err := store.AddIntroduction(ctx, intro)
	if err != nil || !stored {
		t.Fatalf("store introduction: stored %v, error %v", stored, err)
	}
	if _, err := store.Get(ctx, subject.DeviceID()); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("storing a record added a peer: %v", err)
	}
	heads, err := store.IntroductionHeads(ctx)
	if err != nil || len(heads) != 1 || heads[0].Subject != subject.DeviceID() {
		t.Fatalf("stored heads %+v, error %v", heads, err)
	}
	issuerID, err := store.IntroductionIssuer(ctx, subject.DeviceID())
	if err != nil || issuerID != issuer.DeviceID() {
		t.Fatalf("introduction issuer %s, error %v", issuerID, err)
	}
	if _, err := store.IntroductionBySubject(ctx, testIdentity(t, 3).DeviceID()); !errors.Is(err, ErrIntroductionNotFound) {
		t.Fatalf("missing introduction: %v", err)
	}
}

func TestGroupDataKeepsNewest(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	key := bytes.Repeat([]byte{7}, secretSize)
	at := time.UnixMilli(1_760_000_000_000)
	changed, err := store.ApplyGroupData(ctx, GroupData{
		DiscoveryKey: key,
		DiscoveryAt:  at,
		Relay:        RelaySettings{URL: "https://relay.example", Token: "token"},
		RelayAt:      at,
	})
	if err != nil || !changed {
		t.Fatalf("apply group data: changed %v, error %v", changed, err)
	}

	data, err := store.GroupData(ctx)
	if err != nil {
		t.Fatalf("read group data: %v", err)
	}
	if !bytes.Equal(data.DiscoveryKey, key) || !data.DiscoveryAt.Equal(at) {
		t.Fatalf("discovery key was not stored: %+v", data)
	}
	if data.Relay != (RelaySettings{URL: "https://relay.example", Token: "token"}) || !data.RelayAt.Equal(at) {
		t.Fatalf("relay settings were not stored: %+v", data)
	}

	older := bytes.Repeat([]byte{8}, secretSize)
	changed, err = store.ApplyGroupData(ctx, GroupData{
		DiscoveryKey: older,
		DiscoveryAt:  at.Add(-time.Minute),
		Relay:        RelaySettings{URL: "https://old.example", Token: "old"},
		RelayAt:      at.Add(-time.Minute),
	})
	if err != nil || changed {
		t.Fatalf("an older group data was applied: changed %v, error %v", changed, err)
	}
	if data, err := store.GroupData(ctx); err != nil || !bytes.Equal(data.DiscoveryKey, key) {
		t.Fatalf("the discovery key changed: %v, error %v", data.DiscoveryKey, err)
	}
}

func TestGroupDataRefusesFarFutureChange(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	ahead := time.Now().Add(MaxClockSkew + time.Hour)
	_, err := store.ApplyGroupData(ctx, GroupData{
		DiscoveryKey: bytes.Repeat([]byte{1}, secretSize),
		DiscoveryAt:  ahead,
	})
	if !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("a group change far in the future: %v", err)
	}
	if _, err := store.DiscoveryKey(ctx); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("the refused change stored a key: %v", err)
	}
}

func TestGroupDataValidatesSize(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	if _, err := store.ApplyGroupData(ctx, GroupData{DiscoveryKey: []byte{1}}); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("a short discovery key: %v", err)
	}
	long := string(bytes.Repeat([]byte{'a'}, MaxRelayURLLength+1))
	if err := store.SetRelaySettings(ctx, RelaySettings{URL: long}); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("a long relay address: %v", err)
	}
}

func TestSetDiscoveryKeyMarksChange(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	key, err := store.EnsureDiscoveryKey(ctx)
	if err != nil {
		t.Fatalf("ensure discovery key: %v", err)
	}
	first, err := store.GroupData(ctx)
	if err != nil {
		t.Fatalf("read group data: %v", err)
	}
	if !bytes.Equal(first.DiscoveryKey, key) || first.DiscoveryAt.IsZero() {
		t.Fatalf("the key has no change time: %+v", first)
	}

	rotated := bytes.Repeat([]byte{9}, secretSize)
	if err := store.SetDiscoveryKey(ctx, rotated); err != nil {
		t.Fatalf("set discovery key: %v", err)
	}
	second, err := store.GroupData(ctx)
	if err != nil {
		t.Fatalf("read group data: %v", err)
	}
	if !bytes.Equal(second.DiscoveryKey, rotated) {
		t.Fatal("the discovery key was not replaced")
	}
	if !second.DiscoveryAt.After(first.DiscoveryAt) && !second.DiscoveryAt.Equal(first.DiscoveryAt) {
		t.Fatalf("the change time went back: %v", second.DiscoveryAt)
	}
}

func TestDerivedRendezvousKey(t *testing.T) {
	a := testIdentity(t, 1).DeviceID()
	b := testIdentity(t, 2).DeviceID()

	ab := DerivedRendezvousKey(a, b)
	ba := DerivedRendezvousKey(b, a)
	if len(ab) != secretSize {
		t.Fatalf("rendezvous key of %d bytes", len(ab))
	}
	if !slices.Equal(ab, ba) {
		t.Fatal("the two devices derive different rendezvous keys")
	}
	if slices.Equal(ab, DerivedRendezvousKey(a, testIdentity(t, 3).DeviceID())) {
		t.Fatal("different pairs share a rendezvous key")
	}
}
