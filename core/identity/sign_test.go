package identity

import (
	"crypto/mldsa"
	"errors"
	"slices"
	"testing"
)

func TestSignVerify(t *testing.T) {
	id := newTestIdentity(t)
	msg := []byte("hello unison")

	sig, err := id.Sign("pair-request", msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(sig.Ed25519) != 64 {
		t.Fatalf("ed25519 signature: got %d bytes, want 64", len(sig.Ed25519))
	}
	if len(sig.MLDSA) != mldsa.MLDSA65SignatureSize {
		t.Fatalf("mldsa signature: got %d bytes, want %d", len(sig.MLDSA), mldsa.MLDSA65SignatureSize)
	}

	peer, err := NewPublicIdentity(id.Public().Ed25519, id.Public().MLDSABytes())
	if err != nil {
		t.Fatalf("public identity: %v", err)
	}
	if err := peer.Verify("pair-request", msg, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestSignVerifyEmptyMessage(t *testing.T) {
	id := newTestIdentity(t)
	sig, err := id.Sign("revocation", nil)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := id.Public().Verify("revocation", nil, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestSignMLDSAVerify(t *testing.T) {
	id := newTestIdentity(t)
	msg := []byte("channel binding")

	sig, err := id.SignMLDSA("auth", msg)
	if err != nil {
		t.Fatalf("sign mldsa: %v", err)
	}
	if len(sig) != mldsa.MLDSA65SignatureSize {
		t.Fatalf("mldsa signature: got %d bytes, want %d", len(sig), mldsa.MLDSA65SignatureSize)
	}
	if err := id.Public().VerifyMLDSA("auth", msg, sig); err != nil {
		t.Fatalf("verify mldsa: %v", err)
	}

	if err := id.Public().VerifyMLDSA("auth", []byte("other"), sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify other message: got %v, want ErrInvalidSignature", err)
	}
	if err := id.Public().VerifyMLDSA("pair-request", msg, sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify other type: got %v, want ErrInvalidSignature", err)
	}

	sig[0] ^= 0x01
	if err := id.Public().VerifyMLDSA("auth", msg, sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify corrupted signature: got %v, want ErrInvalidSignature", err)
	}
	if err := id.Public().VerifyMLDSA("auth", msg, sig[:16]); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify short signature: got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsCorruptedEd25519(t *testing.T) {
	id := newTestIdentity(t)
	msg := []byte("hello unison")

	sig, err := id.Sign("introduction", msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig.Ed25519[0] ^= 0x01

	err = id.Public().Verify("introduction", msg, sig)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsCorruptedMLDSA(t *testing.T) {
	id := newTestIdentity(t)
	msg := []byte("hello unison")

	sig, err := id.Sign("introduction", msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig.MLDSA[100] ^= 0x01

	err = id.Public().Verify("introduction", msg, sig)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsMissingParts(t *testing.T) {
	id := newTestIdentity(t)
	msg := []byte("hello unison")

	sig, err := id.Sign("revocation", msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	tests := map[string]Signature{
		"no ed25519": {MLDSA: sig.MLDSA},
		"no mldsa":   {Ed25519: sig.Ed25519},
		"short both": {Ed25519: sig.Ed25519[:16], MLDSA: sig.MLDSA[:16]},
	}
	for name, broken := range tests {
		t.Run(name, func(t *testing.T) {
			if err := id.Public().Verify("revocation", msg, broken); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
			}
		})
	}
}

func TestVerifyRejectsOtherMessage(t *testing.T) {
	id := newTestIdentity(t)

	sig, err := id.Sign("revocation", []byte("one"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := id.Public().Verify("revocation", []byte("two"), sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
	}
	if err := id.Public().Verify("revocation", []byte("one!"), sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsOtherType(t *testing.T) {
	id := newTestIdentity(t)
	msg := []byte("hello unison")

	sig, err := id.Sign("pair-request", msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := id.Public().Verify("pair-accept", msg, sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
	}
	if err := id.Public().Verify("pair-request2", msg, sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsOtherKey(t *testing.T) {
	id := newTestIdentity(t)

	edSeed, mldsaSeed := testSeeds()
	edSeed[0] ^= 0xff
	other, err := FromSeeds(edSeed, mldsaSeed)
	if err != nil {
		t.Fatalf("from seeds: %v", err)
	}

	sig, err := id.Sign("revocation", []byte("hello unison"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := other.Public().Verify("revocation", []byte("hello unison"), sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("verify: got %v, want ErrInvalidSignature", err)
	}
}

func TestSignRejectsInvalidType(t *testing.T) {
	id := newTestIdentity(t)

	for _, typ := range []string{"", " ", "pair request", "pair\x00request", "unison/1" + string(make([]byte, 256))} {
		if _, err := id.Sign(typ, nil); !errors.Is(err, ErrInvalidType) {
			t.Fatalf("sign %q: got %v, want ErrInvalidType", typ, err)
		}
	}
	if _, err := id.Sign("pair-request", nil); err != nil {
		t.Fatalf("sign valid type: %v", err)
	}
}

func TestVerifyIncompletePublicIdentity(t *testing.T) {
	id := newTestIdentity(t)

	sig, err := id.Sign("revocation", nil)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	var empty PublicIdentity
	if err := empty.Verify("revocation", nil, sig); err == nil {
		t.Fatal("verify with an empty public identity: got nil error, want an error")
	}
}

func TestEd25519InputIsUnambiguous(t *testing.T) {
	input := ed25519Input("pair", []byte("request"))
	if want := "unison/1 pair\x00request"; string(input) != want {
		t.Fatalf("ed25519 input: got %q, want %q", input, want)
	}
	if !slices.Equal(ed25519Input("pair", nil), []byte("unison/1 pair\x00")) {
		t.Fatalf("ed25519 input without a message: got %q", ed25519Input("pair", nil))
	}
}
