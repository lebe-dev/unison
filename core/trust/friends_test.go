package trust

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

func TestFriendsDiscoveryMode(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	until, err := ts.FriendsDiscoveryUntil(ctx)
	if err != nil {
		t.Fatalf("read the mode: %v", err)
	}
	if !until.IsZero() {
		t.Fatalf("a fresh store reports the mode until %v", until)
	}

	first := time.Now().Add(15 * time.Minute).Truncate(time.Millisecond)
	if err := ts.SetFriendsDiscoveryUntil(ctx, first); err != nil {
		t.Fatalf("set the mode: %v", err)
	}
	until, err = ts.FriendsDiscoveryUntil(ctx)
	if err != nil {
		t.Fatalf("read the mode: %v", err)
	}
	if !until.Equal(first) {
		t.Fatalf("the mode ends at %v, want %v", until, first)
	}

	if err := ts.SetFriendsDiscoveryUntil(ctx, time.Time{}); err != nil {
		t.Fatalf("turn the mode off: %v", err)
	}
	until, err = ts.FriendsDiscoveryUntil(ctx)
	if err != nil {
		t.Fatalf("read the mode: %v", err)
	}
	if !until.IsZero() {
		t.Fatalf("the mode is on until %v after it was turned off", until)
	}
}

func TestFriendsDiscoveryKeys(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)

	friendWithKey := testPeer(t, 30, model.RoleFriends)
	friendWithoutKey := testPeer(t, 31, model.RoleFriends)
	revoked := testPeer(t, 32, model.RoleFriends)
	personal := testPeer(t, 33, model.RolePersonal)
	for _, peer := range []Peer{friendWithKey, friendWithoutKey, revoked, personal} {
		if err := ts.Add(ctx, peer); err != nil {
			t.Fatalf("add peer: %v", err)
		}
	}
	if err := ts.MarkRevoked(ctx, revoked.DeviceID()); err != nil {
		t.Fatalf("revoke peer: %v", err)
	}

	key := bytes.Repeat([]byte{9}, secretSize)
	if err := ts.SetRendezvousKey(ctx, friendWithKey.DeviceID(), key); err != nil {
		t.Fatalf("set rendezvous key: %v", err)
	}

	keys, err := ts.FriendsDiscoveryKeys(ctx)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 || !bytes.Equal(keys[0], key) {
		t.Fatalf("keys = %v, want the one key of the friends device", keys)
	}

	if err := ts.MarkRevoked(ctx, friendWithKey.DeviceID()); err != nil {
		t.Fatalf("revoke the friends device: %v", err)
	}
	keys, err = ts.FriendsDiscoveryKeys(ctx)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("revoked devices left %d keys", len(keys))
	}
}
