package trust

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/store"
)

// openTestStore opens a database with the trust migrations and returns the
// trust store over it.
func openTestStore(t *testing.T) *Store {
	t.Helper()

	trustStore, _ := openTestStoreWithDB(t)
	return trustStore
}

// openTestStoreWithDB opens a database with the trust migrations and returns
// the trust store together with the database.
func openTestStoreWithDB(t *testing.T) (*Store, *store.Store) {
	t.Helper()
	ctx := context.Background()

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	db, err := store.Open(ctx, store.Options{
		Dir:        t.TempDir(),
		Keystore:   fake.NewKeystore(),
		Migrations: migrations,
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	trustStore, err := Open(db)
	if err != nil {
		t.Fatalf("open trust store: %v", err)
	}
	return trustStore, db
}

// testPeer builds a peer whose identity is derived from seed.
func testPeer(t *testing.T, seed byte, role model.Role) Peer {
	t.Helper()

	edSeed := bytes.Repeat([]byte{seed}, ed25519.SeedSize)
	pqSeed := bytes.Repeat([]byte{seed, seed + 1}, mldsa.PrivateKeySize/2)
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	return Peer{
		Public:   id.Public(),
		Name:     fmt.Sprintf("device-%d", seed),
		Platform: model.PlatformMacOS,
		Role:     role,
	}
}

func TestMigrationsStartInTrustBlock(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations")
	}
	if first := migrations[0].Version; first <= migrationsBase || first >= migrationsBase+100 {
		t.Errorf("first version %d is outside the trust block", first)
	}

	_, db := openTestStoreWithDB(t)
	version, err := db.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if want := migrations[len(migrations)-1].Version; version != want {
		t.Errorf("schema version %d, want %d", version, want)
	}
}

func TestOpenRejectsNilStore(t *testing.T) {
	if _, err := Open(nil); err == nil {
		t.Fatal("open without a database: want an error")
	}
}

func TestAddAndGet(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	introducer := testPeer(t, 2, model.RolePersonal)
	peer := testPeer(t, 1, model.RolePersonal)
	peer.Name = "MacBook"
	peer.Platform = model.PlatformMacOS
	peer.RequireConfirmation = true
	peer.IntroducedBy = introducer.Public.DeviceID
	peer.AddedAt = time.UnixMilli(1_700_000_000_000)
	peer.LastSeenAt = time.UnixMilli(1_700_000_100_000)
	peer.LastAddrs = []string{"192.168.1.10:5353"}

	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	got, err := ts.Get(ctx, peer.Public.DeviceID)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	assertPeerEqual(t, got, peer)

	byKey, err := ts.ByEd25519(ctx, peer.Public.Ed25519)
	if err != nil {
		t.Fatalf("get peer by key: %v", err)
	}
	assertPeerEqual(t, byKey, peer)

	if _, err := ts.Get(ctx, introducer.Public.DeviceID); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("get unknown peer: got %v, want ErrPeerNotFound", err)
	}
	if _, err := ts.ByEd25519(ctx, introducer.Public.Ed25519); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("get peer by unknown key: got %v, want ErrPeerNotFound", err)
	}
	if _, err := ts.Get(ctx, model.DeviceID{}); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("get peer with a zero id: got %v, want ErrInvalidPeer", err)
	}
	if _, err := ts.ByEd25519(ctx, []byte("short")); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("get peer by a short key: got %v, want ErrInvalidPeer", err)
	}
}

func TestAddRejectsInvalidPeers(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	valid := testPeer(t, 3, model.RolePersonal)
	withPublic := func(mutate func(*identity.PublicIdentity)) Peer {
		peer := valid
		public := valid.Public
		mutate(&public)
		peer.Public = public
		return peer
	}

	tests := []struct {
		name string
		peer Peer
	}{
		{"missing identity", Peer{Role: model.RolePersonal}},
		{"device id mismatch", withPublic(func(p *identity.PublicIdentity) { p.DeviceID = model.DeviceID{9} })},
		{"unknown role", func() Peer { peer := valid; peer.Role = model.RoleUnspecified; return peer }()},
		{"friends with confirmation", func() Peer {
			peer := valid
			peer.Role = model.RoleFriends
			peer.RequireConfirmation = true
			return peer
		}()},
		{"long name", func() Peer {
			peer := valid
			peer.Name = strings.Repeat("a", maxNameLength+1)
			return peer
		}()},
		{"invalid name", func() Peer { peer := valid; peer.Name = "\xff"; return peer }()},
		{"revoked", func() Peer { peer := valid; peer.Revoked = true; return peer }()},
		{"introduced by itself", func() Peer {
			peer := valid
			peer.IntroducedBy = valid.Public.DeviceID
			return peer
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ts.Add(ctx, test.peer); !errors.Is(err, ErrInvalidPeer) {
				t.Fatalf("add: got %v, want ErrInvalidPeer", err)
			}
		})
	}
}

func TestAddKeepsKnownKeys(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 4, model.RolePersonal)
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	if err := ts.Add(ctx, peer); !errors.Is(err, ErrPeerExists) {
		t.Fatalf("add the same peer: got %v, want ErrPeerExists", err)
	}

	// A record with the known Ed25519 key and another ML-DSA key must not pass.
	other := testPeer(t, 5, model.RoleFriends)
	public, err := identity.NewPublicIdentity(peer.Public.Ed25519, other.Public.MLDSABytes())
	if err != nil {
		t.Fatalf("build identity: %v", err)
	}
	impostor := Peer{Public: public, Name: "impostor", Role: model.RoleFriends}
	if err := ts.Add(ctx, impostor); !errors.Is(err, ErrPeerExists) {
		t.Fatalf("add a known key: got %v, want ErrPeerExists", err)
	}
}

func TestRevocationIsPermanent(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 6, model.RolePersonal)
	peer.LastAddrs = []string{"192.168.1.20:5353"}
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	id := peer.Public.DeviceID

	if err := ts.MarkRevoked(ctx, id); err != nil {
		t.Fatalf("revoke peer: %v", err)
	}
	got, err := ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get revoked peer: %v", err)
	}
	if !got.Revoked {
		t.Fatal("revoked peer is not marked revoked")
	}
	if len(got.LastAddrs) != 0 {
		t.Fatalf("revoked peer kept %d addresses", len(got.LastAddrs))
	}
	if got.Allowed(ActionSyncHistory) {
		t.Fatal("a revoked peer is allowed to sync history")
	}

	// Revocation is idempotent, so a repeated revocation is not an error.
	if err := ts.MarkRevoked(ctx, id); err != nil {
		t.Fatalf("revoke twice: %v", err)
	}

	if err := ts.Add(ctx, peer); !errors.Is(err, ErrPeerRevoked) {
		t.Fatalf("add a revoked key: got %v, want ErrPeerRevoked", err)
	}

	updates := []struct {
		name string
		fn   func() error
	}{
		{"rename", func() error { return ts.Rename(ctx, id, "old") }},
		{"require confirmation", func() error { return ts.SetRequireConfirmation(ctx, id, true) }},
		{"touch seen", func() error { return ts.TouchSeen(ctx, id, time.Now()) }},
		{"update addresses", func() error { return ts.UpdateAddrs(ctx, id, []string{"192.168.1.20:5353"}) }},
	}
	for _, update := range updates {
		t.Run(update.name, func(t *testing.T) {
			if err := update.fn(); !errors.Is(err, ErrPeerRevoked) {
				t.Fatalf("update revoked peer: got %v, want ErrPeerRevoked", err)
			}
		})
	}

	byKey, err := ts.ByEd25519(ctx, peer.Public.Ed25519)
	if err != nil {
		t.Fatalf("get revoked peer by key: %v", err)
	}
	if !byKey.Revoked {
		t.Fatal("revoked peer found by key is not marked revoked")
	}

	revokeUnknown := testPeer(t, 7, model.RolePersonal)
	if err := ts.MarkRevoked(ctx, revokeUnknown.Public.DeviceID); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("revoke unknown peer: got %v, want ErrPeerNotFound", err)
	}
	if err := ts.MarkRevoked(ctx, model.DeviceID{}); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("revoke a zero id: got %v, want ErrInvalidPeer", err)
	}
}

func TestListFiltersByRole(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	personal := testPeer(t, 8, model.RolePersonal)
	friends := testPeer(t, 9, model.RoleFriends)
	revoked := testPeer(t, 10, model.RolePersonal)
	for _, peer := range []Peer{personal, friends, revoked} {
		if err := ts.Add(ctx, peer); err != nil {
			t.Fatalf("add peer: %v", err)
		}
	}
	if err := ts.MarkRevoked(ctx, revoked.Public.DeviceID); err != nil {
		t.Fatalf("revoke peer: %v", err)
	}

	tests := []struct {
		name   string
		filter ListFilter
		want   []model.DeviceID
	}{
		{"all active", ListFilter{}, []model.DeviceID{personal.Public.DeviceID, friends.Public.DeviceID}},
		{"personal", ListFilter{Role: model.RolePersonal}, []model.DeviceID{personal.Public.DeviceID}},
		{"friends", ListFilter{Role: model.RoleFriends}, []model.DeviceID{friends.Public.DeviceID}},
		{"with revoked", ListFilter{IncludeRevoked: true}, []model.DeviceID{
			personal.Public.DeviceID, friends.Public.DeviceID, revoked.Public.DeviceID,
		}},
		{"revoked personal", ListFilter{Role: model.RolePersonal, IncludeRevoked: true}, []model.DeviceID{
			personal.Public.DeviceID, revoked.Public.DeviceID,
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			peers, err := ts.List(ctx, test.filter)
			if err != nil {
				t.Fatalf("list peers: %v", err)
			}
			got := make(map[model.DeviceID]bool, len(peers))
			for _, peer := range peers {
				got[peer.Public.DeviceID] = true
			}
			if len(peers) != len(test.want) {
				t.Fatalf("listed %d peers, want %d", len(peers), len(test.want))
			}
			for _, id := range test.want {
				if !got[id] {
					t.Errorf("peer %s is missing from the list", id)
				}
			}
		})
	}
}

func TestUpdateAddrsBoundsCache(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 11, model.RolePersonal)
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	id := peer.Public.DeviceID

	offered := make([]string, 0, MaxAddrs+4)
	for i := range MaxAddrs + 4 {
		offered = append(offered, fmt.Sprintf("10.0.0.%d:5353", i))
	}
	if err := ts.UpdateAddrs(ctx, id, offered); err != nil {
		t.Fatalf("update addresses: %v", err)
	}
	got, err := ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if len(got.LastAddrs) != MaxAddrs {
		t.Fatalf("cached %d addresses, want %d", len(got.LastAddrs), MaxAddrs)
	}
	for i, addr := range got.LastAddrs {
		if addr != offered[i] {
			t.Errorf("address %d is %q, want %q", i, addr, offered[i])
		}
	}

	if err := ts.UpdateAddrs(ctx, id, []string{" 10.1.1.1:5353 ", "10.1.1.1:5353", "", "10.1.1.2:5353"}); err != nil {
		t.Fatalf("update addresses: %v", err)
	}
	got, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	want := []string{"10.1.1.1:5353", "10.1.1.2:5353"}
	if !slices.Equal(got.LastAddrs, want) {
		t.Fatalf("addresses %q, want %q", got.LastAddrs, want)
	}

	if err := ts.UpdateAddrs(ctx, id, nil); err != nil {
		t.Fatalf("clear addresses: %v", err)
	}
	got, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if len(got.LastAddrs) != 0 {
		t.Fatalf("cleared peer kept %q", got.LastAddrs)
	}
}

func TestRenameAndConfirmation(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 12, model.RolePersonal)
	friends := testPeer(t, 13, model.RoleFriends)
	for _, p := range []Peer{peer, friends} {
		if err := ts.Add(ctx, p); err != nil {
			t.Fatalf("add peer: %v", err)
		}
	}
	id := peer.Public.DeviceID

	if err := ts.Rename(ctx, id, "Desk"); err != nil {
		t.Fatalf("rename peer: %v", err)
	}
	got, err := ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if got.Name != "Desk" {
		t.Fatalf("name %q, want Desk", got.Name)
	}
	if !got.NameLocal {
		t.Error("the name of the user is not marked local")
	}
	if err := ts.SetAnnouncedName(ctx, id, "Mug"); err != nil {
		t.Fatalf("record the announced name: %v", err)
	}
	got, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if got.Name != "Desk" {
		t.Errorf("the announced name replaced the local one with %q", got.Name)
	}
	if err := ts.SetAnnouncedName(ctx, friends.Public.DeviceID, "Ann"); err != nil {
		t.Fatalf("record the announced name: %v", err)
	}
	got, err = ts.Get(ctx, friends.Public.DeviceID)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if got.Name != "Ann" {
		t.Errorf("the announced name is %q, want Ann", got.Name)
	}
	if err := ts.Rename(ctx, id, strings.Repeat("a", maxNameLength+1)); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("rename to a long name: got %v, want ErrInvalidPeer", err)
	}
	if err := ts.Rename(ctx, friends.Public.DeviceID, "\xff"); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("rename to an invalid name: got %v, want ErrInvalidPeer", err)
	}
	unknown := testPeer(t, 14, model.RolePersonal)
	if err := ts.Rename(ctx, unknown.Public.DeviceID, "ghost"); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("rename unknown peer: got %v, want ErrPeerNotFound", err)
	}

	if err := ts.SetRequireConfirmation(ctx, id, true); err != nil {
		t.Fatalf("set confirmation: %v", err)
	}
	got, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if !got.RequireConfirmation {
		t.Fatal("confirmation is not set")
	}
	if got.Decision(ActionApplyClipboard) != Confirm {
		t.Fatalf("clipboard decision %s, want confirm", got.Decision(ActionApplyClipboard))
	}
	if err := ts.SetRequireConfirmation(ctx, id, false); err != nil {
		t.Fatalf("clear confirmation: %v", err)
	}
	if err := ts.SetRequireConfirmation(ctx, friends.Public.DeviceID, true); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("confirm a friends peer: got %v, want ErrInvalidPeer", err)
	}
	if err := ts.SetRequireConfirmation(ctx, friends.Public.DeviceID, false); err != nil {
		t.Fatalf("clear confirmation of a friends peer: %v", err)
	}
}

// TestSetRequireConfirmationRemote checks that the announced flag of a peer is
// stored separately from the local policy for it.
func TestSetRequireConfirmationRemote(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 15, model.RolePersonal)
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	id := peer.DeviceID()

	if err := ts.SetRequireConfirmation(ctx, id, true); err != nil {
		t.Fatalf("set the local policy: %v", err)
	}
	if err := ts.SetRequireConfirmationRemote(ctx, id, true); err != nil {
		t.Fatalf("set the announced flag: %v", err)
	}
	got, err := ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if !got.RequireConfirmationRemote {
		t.Fatal("the announced flag is not stored")
	}
	if !got.RequireConfirmation {
		t.Fatal("the announced flag cleared the local policy")
	}

	if err := ts.SetRequireConfirmationRemote(ctx, id, false); err != nil {
		t.Fatalf("clear the announced flag: %v", err)
	}
	got, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if got.RequireConfirmationRemote {
		t.Fatal("the announced flag is still stored")
	}
	if !got.RequireConfirmation {
		t.Fatal("clearing the announced flag changed the local policy")
	}

	if err := ts.SetRequireConfirmationRemote(ctx, testPeer(t, 16, model.RolePersonal).DeviceID(), true); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("set the flag of an unknown peer: got %v, want ErrPeerNotFound", err)
	}
}

// TestSetPlatform checks that the platform a peer announces replaces the
// platform recorded at pairing.
func TestSetPlatform(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 17, model.RolePersonal)
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	if err := ts.SetPlatform(ctx, peer.DeviceID(), model.PlatformAndroid); err != nil {
		t.Fatalf("set platform: %v", err)
	}
	got, err := ts.Get(ctx, peer.DeviceID())
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if got.Platform != model.PlatformAndroid {
		t.Fatalf("platform %s, want android", got.Platform)
	}
	if err := ts.SetPlatform(ctx, peer.DeviceID(), model.PlatformUnknown); !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("set an unknown platform: got %v, want ErrInvalidPeer", err)
	}
}

func TestTouchSeen(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	peer := testPeer(t, 15, model.RolePersonal)
	peer.AddedAt = time.UnixMilli(1_700_000_000_000)
	peer.LastAddrs = []string{"192.168.1.30:5353"}
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	id := peer.Public.DeviceID

	seen := peer.AddedAt.Add(time.Hour)
	if err := ts.TouchSeen(ctx, id, seen); err != nil {
		t.Fatalf("touch seen: %v", err)
	}
	got, err := ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if got.LastSeenAt.UnixMilli() != seen.UnixMilli() {
		t.Fatalf("last seen %v, want %v", got.LastSeenAt, seen)
	}
	if got.AddedAt.UnixMilli() != peer.AddedAt.UnixMilli() {
		t.Fatalf("added at %v, want %v", got.AddedAt, peer.AddedAt)
	}
	if !slices.Equal(got.LastAddrs, peer.LastAddrs) {
		t.Fatalf("addresses %q, want %q", got.LastAddrs, peer.LastAddrs)
	}

	if err := ts.TouchSeen(ctx, id, time.Time{}); err != nil {
		t.Fatalf("touch seen without a time: %v", err)
	}
	got, err = ts.Get(ctx, id)
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if !got.LastSeenAt.After(seen) {
		t.Fatalf("last seen %v, want a newer time", got.LastSeenAt)
	}

	unknown := testPeer(t, 16, model.RolePersonal)
	if err := ts.TouchSeen(ctx, unknown.Public.DeviceID, seen); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("touch unknown peer: got %v, want ErrPeerNotFound", err)
	}
}

func TestPeersAreSealedOnDisk(t *testing.T) {
	ctx := context.Background()
	ts, db := openTestStoreWithDB(t)

	const name = "secret-laptop-name"
	const addr = "203.0.113.9:5353"

	peer := testPeer(t, 17, model.RolePersonal)
	peer.Name = name
	peer.LastAddrs = []string{addr}
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	var sealedProfile, sealedAddrs []byte
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT profile, addrs FROM peers WHERE device_id = ?", peer.Public.DeviceID.Bytes()).
			Scan(&sealedProfile, &sealedAddrs)
	}); err != nil {
		t.Fatalf("read columns: %v", err)
	}
	if bytes.Contains(sealedProfile, []byte(name)) {
		t.Fatal("the profile column contains the name")
	}
	if bytes.Contains(sealedAddrs, []byte(addr)) {
		t.Fatal("the address column contains the address")
	}

	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	for _, file := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if bytes.Contains(data, []byte(name)) {
			t.Fatalf("%s contains the name", file)
		}
		if bytes.Contains(data, []byte(addr)) {
			t.Fatalf("%s contains the address", file)
		}
	}
}

func TestSealedColumnsAreBoundToTheirColumn(t *testing.T) {
	ctx := context.Background()
	ts, db := openTestStoreWithDB(t)

	peer := testPeer(t, 18, model.RolePersonal)
	if err := ts.Add(ctx, peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	// A profile sealed for its own column must not open as an address cache.
	moved, err := db.Sealer().Seal(peersTable, peerRowKey(peer.Public.DeviceID, profileColumn), []byte(`{"version":1}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE peers SET addrs = ? WHERE device_id = ?", moved, peer.Public.DeviceID.Bytes())
		return err
	}); err != nil {
		t.Fatalf("move column: %v", err)
	}

	if _, err := ts.Get(ctx, peer.Public.DeviceID); !errors.Is(err, store.ErrDecrypt) {
		t.Fatalf("get peer with a moved column: got %v, want ErrDecrypt", err)
	}
}

// assertPeerEqual compares the fields that the store must preserve.
func assertPeerEqual(t *testing.T, got, want Peer) {
	t.Helper()

	if got.Public.DeviceID != want.Public.DeviceID {
		t.Errorf("device id %s, want %s", got.Public.DeviceID, want.Public.DeviceID)
	}
	if !bytes.Equal(got.Public.Ed25519, want.Public.Ed25519) {
		t.Error("ed25519 key differs")
	}
	if !bytes.Equal(got.Public.MLDSABytes(), want.Public.MLDSABytes()) {
		t.Error("mldsa key differs")
	}
	if got.Name != want.Name {
		t.Errorf("name %q, want %q", got.Name, want.Name)
	}
	if got.Platform != want.Platform {
		t.Errorf("platform %s, want %s", got.Platform, want.Platform)
	}
	if got.Role != want.Role {
		t.Errorf("role %s, want %s", got.Role, want.Role)
	}
	if got.RequireConfirmation != want.RequireConfirmation {
		t.Errorf("confirmation %t, want %t", got.RequireConfirmation, want.RequireConfirmation)
	}
	if got.IntroducedBy != want.IntroducedBy {
		t.Errorf("introduced by %s, want %s", got.IntroducedBy, want.IntroducedBy)
	}
	if got.AddedAt.UnixMilli() != want.AddedAt.UnixMilli() {
		t.Errorf("added at %v, want %v", got.AddedAt, want.AddedAt)
	}
	if got.LastSeenAt.UnixMilli() != want.LastSeenAt.UnixMilli() {
		t.Errorf("last seen %v, want %v", got.LastSeenAt, want.LastSeenAt)
	}
	if !slices.Equal(got.LastAddrs, want.LastAddrs) {
		t.Errorf("addresses %q, want %q", got.LastAddrs, want.LastAddrs)
	}
	if got.Revoked != want.Revoked {
		t.Errorf("revoked %t, want %t", got.Revoked, want.Revoked)
	}
}
