package identity

import (
	"crypto/mldsa"
	"slices"
	"testing"
)

func TestPublicIdentityWireRoundTrip(t *testing.T) {
	id := newTestIdentity(t)
	public := id.Public()

	encoded, err := public.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(encoded) != publicIdentitySize {
		t.Fatalf("encoding: got %d bytes, want %d", len(encoded), publicIdentitySize)
	}

	decoded, err := UnmarshalPublicIdentity(encoded)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.DeviceID != public.DeviceID {
		t.Fatalf("device id: got %s, want %s", decoded.DeviceID, public.DeviceID)
	}
	if !slices.Equal(decoded.Ed25519, public.Ed25519) {
		t.Fatal("ed25519 public key changed")
	}
	if !slices.Equal(decoded.MLDSABytes(), public.MLDSABytes()) {
		t.Fatal("mldsa public key changed")
	}
	if decoded.Fingerprint() != public.Fingerprint() {
		t.Fatalf("fingerprint: got %s, want %s", decoded.Fingerprint(), public.Fingerprint())
	}
}

func TestPublicIdentityWireRejectsBadInput(t *testing.T) {
	public := newTestIdentity(t).Public()
	encoded, err := public.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	wrongVersion := slices.Clone(encoded)
	wrongVersion[0] = 2
	wrongID := slices.Clone(encoded)
	wrongID[1] ^= 0xff
	wrongKey := slices.Clone(encoded)
	wrongKey[1+deviceIDSize] ^= 0xff

	tests := map[string][]byte{
		"empty":         nil,
		"truncated":     encoded[:len(encoded)-1],
		"long":          append(slices.Clone(encoded), 0),
		"wrong version": wrongVersion,
		"wrong id":      wrongID,
		"wrong key":     wrongKey,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := UnmarshalPublicIdentity(data); err == nil {
				t.Fatal("unmarshal: got nil error, want an error")
			}
		})
	}
}

func TestMarshalIncompletePublicIdentity(t *testing.T) {
	var empty PublicIdentity
	if _, err := empty.MarshalBinary(); err == nil {
		t.Fatal("marshal empty identity: got nil error, want an error")
	}

	short := PublicIdentity{Ed25519: make([]byte, 16), MLDSA65: newTestIdentity(t).Public().MLDSA65}
	if _, err := short.MarshalBinary(); err == nil {
		t.Fatal("marshal short ed25519 key: got nil error, want an error")
	}
}

func TestNewPublicIdentityRejectsBadKeys(t *testing.T) {
	public := newTestIdentity(t).Public()

	if _, err := NewPublicIdentity(make([]byte, 16), public.MLDSABytes()); err == nil {
		t.Fatal("short ed25519 key: got nil error, want an error")
	}
	if _, err := NewPublicIdentity(public.Ed25519, make([]byte, mldsa.MLDSA65PublicKeySize-1)); err == nil {
		t.Fatal("short mldsa key: got nil error, want an error")
	}

	copied, err := NewPublicIdentity(public.Ed25519, public.MLDSABytes())
	if err != nil {
		t.Fatalf("new public identity: %v", err)
	}
	if copied.DeviceID != public.DeviceID {
		t.Fatalf("device id: got %s, want %s", copied.DeviceID, public.DeviceID)
	}

	// The constructor must not keep references to the caller bytes.
	edPub := slices.Clone(public.Ed25519)
	pqPub := public.MLDSABytes()
	kept, err := NewPublicIdentity(edPub, pqPub)
	if err != nil {
		t.Fatalf("new public identity: %v", err)
	}
	edPub[0] ^= 0xff
	pqPub[0] ^= 0xff
	if !slices.Equal(kept.Ed25519, public.Ed25519) || !slices.Equal(kept.MLDSABytes(), public.MLDSABytes()) {
		t.Fatal("public identity aliases the caller bytes")
	}
	if kept.DeviceID != public.DeviceID {
		t.Fatal("device id does not match the copied keys")
	}
}

func TestMLDSABytesNilKey(t *testing.T) {
	var empty PublicIdentity
	if empty.MLDSABytes() != nil {
		t.Fatal("mldsa bytes of an empty identity: got data, want nil")
	}
}

func TestDeviceIDFromKeysUsesBothKeys(t *testing.T) {
	public := newTestIdentity(t).Public()
	pqBytes := public.MLDSABytes()

	base := DeviceIDFromKeys(public.Ed25519, pqBytes)

	otherEd := slices.Clone(public.Ed25519)
	otherEd[0] ^= 0xff
	if DeviceIDFromKeys(otherEd, pqBytes) == base {
		t.Fatal("device id ignores the ed25519 key")
	}

	otherPQ := slices.Clone(pqBytes)
	otherPQ[0] ^= 0xff
	if DeviceIDFromKeys(public.Ed25519, otherPQ) == base {
		t.Fatal("device id ignores the mldsa key")
	}
}
