package mobile

import (
	"crypto/ed25519"
	"testing"
)

// TestVerifyReleaseSignature accepts a signature the release tool writes.
func TestVerifyReleaseSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	payload := []byte("checksums of the release\n")
	sig := ed25519.Sign(priv, append([]byte(releaseSignatureHead), payload...))

	if !VerifyReleaseSignature(pub, payload, sig) {
		t.Fatal("a valid signature was refused")
	}
}

// TestVerifyReleaseSignatureRefusesChanges checks the payload and the key.
func TestVerifyReleaseSignatureRefusesChanges(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	payload := []byte("checksums of the release\n")
	sig := ed25519.Sign(priv, append([]byte(releaseSignatureHead), payload...))

	if VerifyReleaseSignature(pub, []byte("checksums of another release\n"), sig) {
		t.Fatal("a changed payload was accepted")
	}
	other, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if VerifyReleaseSignature(other, payload, sig) {
		t.Fatal("a signature of another key was accepted")
	}
}

// TestVerifyReleaseSignatureRefusesTheBarePayload checks the domain separation:
// a signature over the manifest alone does not sign a release.
func TestVerifyReleaseSignatureRefusesTheBarePayload(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	payload := []byte("checksums of the release\n")
	sig := ed25519.Sign(priv, payload)

	if VerifyReleaseSignature(pub, payload, sig) {
		t.Fatal("a signature without the domain separation was accepted")
	}
}

// TestVerifyReleaseSignatureRefusesWrongSizes checks the sizes of the inputs.
func TestVerifyReleaseSignatureRefusesWrongSizes(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	payload := []byte("checksums of the release\n")
	sig := ed25519.Sign(priv, append([]byte(releaseSignatureHead), payload...))

	if VerifyReleaseSignature(pub[:16], payload, sig) {
		t.Fatal("a short public key was accepted")
	}
	if VerifyReleaseSignature(pub, payload, sig[:32]) {
		t.Fatal("a short signature was accepted")
	}
	if VerifyReleaseSignature(nil, payload, nil) {
		t.Fatal("empty inputs were accepted")
	}
}
