package mediate

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// newTestIdentity returns an identity from random seeds.
func newTestIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	edSeed := make([]byte, 32)
	pqSeed := make([]byte, 32)
	if _, err := rand.Read(edSeed); err != nil {
		t.Fatalf("ed25519 seed: %v", err)
	}
	if _, err := rand.Read(pqSeed); err != nil {
		t.Fatalf("ml-dsa seed: %v", err)
	}
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return id
}

// TestRendezvousKeyMatches checks that both endpoints derive the same key from
// the exchange.
func TestRendezvousKeyMatches(t *testing.T) {
	low := newTestIdentity(t)
	high := newTestIdentity(t)
	var sessionID [sessionIDSize]byte
	if _, err := rand.Read(sessionID[:]); err != nil {
		t.Fatalf("session id: %v", err)
	}
	mediator := newTestIdentity(t)
	lowID, highID := orderDevices(low.DeviceID(), high.DeviceID())
	base := offerTranscript(sessionID, mediator.DeviceID(), lowID, highID, model.RolePersonal, 1_700_000_000_000)

	lowKeys, err := newEphemeralKeys()
	if err != nil {
		t.Fatalf("low keys: %v", err)
	}
	defer lowKeys.destroy()
	highShare, err := encapsulate(lowKeys.xPub, lowKeys.ek)
	if err != nil {
		t.Fatalf("encapsulate: %v", err)
	}
	defer highShare.destroy()

	highKey := deriveRendezvousKey(base, lowKeys.xPub, lowKeys.ek, highShare.xPub, highShare.mlkemCT, highShare.sharedX, highShare.sharedPQ)
	sharedX, sharedPQ, err := decapsulate(lowKeys, highShare.xPub, highShare.mlkemCT)
	if err != nil {
		t.Fatalf("decapsulate: %v", err)
	}
	defer clear(sharedX)
	defer clear(sharedPQ)
	lowKey := deriveRendezvousKey(base, lowKeys.xPub, lowKeys.ek, highShare.xPub, highShare.mlkemCT, sharedX, sharedPQ)

	if len(lowKey) != keySize {
		t.Fatalf("rendezvous key of %d bytes, want %d", len(lowKey), keySize)
	}
	if !bytes.Equal(lowKey, highKey) {
		t.Fatal("the endpoints derive different rendezvous keys")
	}
	if bytes.Equal(lowKey, base) {
		t.Fatal("the rendezvous key repeats the transcript")
	}
}

// TestTranscriptsBindFields checks that a mediator cannot change a field of the
// signed transcript without failing verification.
func TestTranscriptsBindFields(t *testing.T) {
	low := newTestIdentity(t)
	high := newTestIdentity(t)
	mediator := newTestIdentity(t)
	var sessionID [sessionIDSize]byte
	if _, err := rand.Read(sessionID[:]); err != nil {
		t.Fatalf("session id: %v", err)
	}
	lowID, highID := orderDevices(low.DeviceID(), high.DeviceID())
	base := offerTranscript(sessionID, mediator.DeviceID(), lowID, highID, model.RoleFriends, 1_700_000_000_000)
	signature, err := mediator.Sign(OfferSignatureType, base)
	if err != nil {
		t.Fatalf("sign offer: %v", err)
	}
	if err := mediator.Public().Verify(OfferSignatureType, base, signature); err != nil {
		t.Fatalf("verify offer: %v", err)
	}

	changed := offerTranscript(sessionID, mediator.DeviceID(), lowID, highID, model.RolePersonal, 1_700_000_000_000)
	if err := mediator.Public().Verify(OfferSignatureType, changed, signature); err == nil {
		t.Fatal("a changed role keeps the offer signature valid")
	}
	changed = offerTranscript(sessionID, mediator.DeviceID(), lowID, highID, model.RoleFriends, 1_700_000_000_001)
	if err := mediator.Public().Verify(OfferSignatureType, changed, signature); err == nil {
		t.Fatal("a changed deadline keeps the offer signature valid")
	}
	changed = offerTranscript(sessionID, high.DeviceID(), lowID, highID, model.RoleFriends, 1_700_000_000_000)
	if err := mediator.Public().Verify(OfferSignatureType, changed, signature); err == nil {
		t.Fatal("a changed mediator keeps the offer signature valid")
	}
}

// TestSASMatches checks that both endpoints compute the same verification code.
func TestSASMatches(t *testing.T) {
	low := newTestIdentity(t)
	high := newTestIdentity(t)
	mediator := newTestIdentity(t)
	var sessionID [sessionIDSize]byte
	if _, err := rand.Read(sessionID[:]); err != nil {
		t.Fatalf("session id: %v", err)
	}
	lowID, _ := orderDevices(low.DeviceID(), high.DeviceID())
	lowPublic, highPublic := low.Public(), high.Public()
	if lowID != lowPublic.DeviceID {
		lowPublic, highPublic = highPublic, lowPublic
	}
	one := deriveSAS(sessionID, mediator.DeviceID(), lowPublic, highPublic, model.RoleFriends)
	other := deriveSAS(sessionID, mediator.DeviceID(), lowPublic, highPublic, model.RoleFriends)
	if one.String() != other.String() {
		t.Fatalf("the verification codes differ: %s and %s", one, other)
	}
	if one.IsZero() {
		t.Fatal("the verification code is empty")
	}
	changed := deriveSAS(sessionID, mediator.DeviceID(), lowPublic, highPublic, model.RolePersonal)
	if changed.String() == one.String() {
		t.Fatal("the role does not change the verification code")
	}
}
