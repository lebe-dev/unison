package mediate

import (
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

// TestReplayCacheRemembersIdentifiers checks that a reused session identifier
// is refused within the window.
func TestReplayCacheRemembersIdentifiers(t *testing.T) {
	cache := &replayCache{}
	var id [sessionIDSize]byte
	id[0] = 1
	if cache.seen(id) {
		t.Fatal("an unused identifier is reported as used")
	}
	cache.add(id)
	if !cache.seen(id) {
		t.Fatal("a used identifier is forgotten")
	}
	id[0] = 2
	if cache.seen(id) {
		t.Fatal("another identifier is reported as used")
	}
}

// TestReplayCacheDropsTheOldestEntry checks that the cache stays bounded.
func TestReplayCacheDropsTheOldestEntry(t *testing.T) {
	cache := &replayCache{}
	for i := 0; i < ReplayCacheSize+8; i++ {
		var id [sessionIDSize]byte
		id[0] = byte(i)
		id[1] = byte(i >> 8)
		cache.add(id)
	}
	if len(cache.entries) > ReplayCacheSize {
		t.Fatalf("the cache holds %d entries, limit %d", len(cache.entries), ReplayCacheSize)
	}
	var first [sessionIDSize]byte
	if cache.seen(first) {
		t.Fatal("the oldest entry was kept, so the cache did not prune")
	}
}

// TestOfferRateLimit checks the offer rate of one mediator.
func TestOfferRateLimit(t *testing.T) {
	service := &Service{rates: make(map[model.DeviceID]*rateBucket)}
	var mediator model.DeviceID
	mediator[0] = 7
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < OfferBurst; i++ {
		if !service.allowLocked(mediator, now) {
			t.Fatalf("offer %d is refused inside the burst", i)
		}
	}
	if service.allowLocked(mediator, now) {
		t.Fatal("an offer beyond the burst is allowed")
	}
	if !service.allowLocked(mediator, now.Add(OfferRate)) {
		t.Fatal("an offer after the interval is refused")
	}
	other := mediator
	other[1] = 9
	if !service.allowLocked(other, now) {
		t.Fatal("another mediator shares the bucket")
	}
}

// TestRoleWithin checks the monotonic role rule.
func TestRoleWithin(t *testing.T) {
	tests := []struct {
		role, mediator model.Role
		want           bool
	}{
		{model.RolePersonal, model.RolePersonal, true},
		{model.RoleFriends, model.RolePersonal, true},
		{model.RolePersonal, model.RoleFriends, false},
		{model.RoleFriends, model.RoleFriends, true},
		{model.RoleFriends, model.RoleUnspecified, false},
		{model.RoleUnspecified, model.RolePersonal, false},
	}
	for _, test := range tests {
		if got := roleWithin(test.role, test.mediator); got != test.want {
			t.Errorf("%s through %s: %t, want %t", test.role, test.mediator, got, test.want)
		}
	}
}

// TestOrderDevices checks the canonical order of the endpoints.
func TestOrderDevices(t *testing.T) {
	var one, other model.DeviceID
	one[0], other[0] = 1, 2
	low, high := orderDevices(other, one)
	if low != one || high != other {
		t.Fatal("the pair is not ordered by device id")
	}
	if !samePair(one, other, other, one) {
		t.Fatal("the pair depends on the order")
	}
}
