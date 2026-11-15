package identity

import (
	"bytes"
	"crypto/mldsa"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// Fixed inputs and expected outputs of the identity scheme. They pin the device
// ID derivation, the signing inputs and the certificate contents, so any change
// in these is a deliberate protocol change.
const (
	vectorEdSeed        = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	vectorMLDSASeed     = "808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f"
	vectorEd25519Pub    = "79b5562e8fe654f94078b112e8a98ba7901f853ae695bed7e0e3910bad049664"
	vectorMLDSAPubHash  = "e00c3ad05e18901d30ebc2c9044f4b0756ae6922ff258d688292e8ead8d4a2d5"
	vectorDeviceID      = "539fd7d967b66cd7e8f21041f401877bdc4798c415cb764e0f5461366b9ca02a"
	vectorFingerprint   = "KOP5-PWLH-WZWN-P2HS"
	vectorType          = "test-vector"
	vectorMessage       = "hello unison"
	vectorEd25519Sig    = "0c97f6d36ce082ad877ecce7e423fb5d891b0437cb7e622b82ae0303791d2114ade78ea3a0f337fea00d324743577dd966d713cadd58a5003030142bb6587f02"
	vectorMLDSADetHash  = "f50d12945b72d449514a4b2c75e55acf2dd2e4d5388ba38223b751405755cd80"
	vectorPublicIDHash  = "797ce3d8df5f8369c7f190fb41238f32513e5f27ba965530d3637caa93fdc0f3"
	vectorCertFP        = "83f82603b81cfd25d0bfd6e4aca353bd86bae2fdc2597ab1306b34e7740e8b82"
	vectorCertSerial    = "539fd7d967b66cd7e8f21041f401877b"
	vectorCertNotBefore = "2025-11-15T08:46:40Z"
	vectorCertNotAfter  = "2035-11-13T09:46:40Z"
)

// vectorCertTime is the creation time used for the certificate vectors.
var vectorCertTime = time.Date(2025, 11, 15, 9, 46, 40, 0, time.UTC)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return b
}

func TestVectors(t *testing.T) {
	id, err := FromSeeds(mustHex(t, vectorEdSeed), mustHex(t, vectorMLDSASeed))
	if err != nil {
		t.Fatalf("from seeds: %v", err)
	}
	public := id.Public()

	if got, want := hex.EncodeToString(public.Ed25519), vectorEd25519Pub; got != want {
		t.Fatalf("ed25519 public key: got %s, want %s", got, want)
	}
	if got, want := sha256.Sum256(public.MLDSABytes()), mustHex(t, vectorMLDSAPubHash); !bytes.Equal(got[:], want) {
		t.Fatalf("mldsa public key hash: got %x, want %x", got, want)
	}
	if got, want := id.DeviceID().String(), vectorDeviceID; got != want {
		t.Fatalf("device id: got %s, want %s", got, want)
	}
	if got, want := string(id.Fingerprint()), vectorFingerprint; got != want {
		t.Fatalf("fingerprint: got %s, want %s", got, want)
	}

	sig, err := id.Sign(vectorType, []byte(vectorMessage))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if got, want := hex.EncodeToString(sig.Ed25519), vectorEd25519Sig; got != want {
		t.Fatalf("ed25519 signature: got %s, want %s", got, want)
	}
	if err := public.Verify(vectorType, []byte(vectorMessage), sig); err != nil {
		t.Fatalf("verify: %v", err)
	}

	det, err := id.pqKey.SignDeterministic([]byte(vectorMessage), &mldsa.Options{Context: mldsaContext(vectorType)})
	if err != nil {
		t.Fatalf("sign deterministic mldsa: %v", err)
	}
	if got, want := sha256.Sum256(det), mustHex(t, vectorMLDSADetHash); !bytes.Equal(got[:], want) {
		t.Fatalf("mldsa signature hash: got %x, want %x", got, want)
	}
	if err := public.Verify(vectorType, []byte(vectorMessage), Signature{Ed25519: sig.Ed25519, MLDSA: det}); err != nil {
		t.Fatalf("verify deterministic mldsa: %v", err)
	}

	wire, err := public.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := sha256.Sum256(wire), mustHex(t, vectorPublicIDHash); !bytes.Equal(got[:], want) {
		t.Fatalf("public identity hash: got %x, want %x", got, want)
	}
	if got, want := len(wire), publicIdentitySize; got != want {
		t.Fatalf("public identity length: got %d, want %d", got, want)
	}

	cert, err := newCertificate(id.edKey, id.DeviceID(), vectorCertTime)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	if got, want := CertFingerprintOfDER(cert.Certificate[0]).String(), vectorCertFP; got != want {
		t.Fatalf("certificate fingerprint: got %s, want %s", got, want)
	}
	if got, want := cert.Leaf.SerialNumber.Text(16), vectorCertSerial; got != want {
		t.Fatalf("certificate serial: got %s, want %s", got, want)
	}
	if got, want := cert.Leaf.NotBefore.UTC().Format(time.RFC3339), vectorCertNotBefore; got != want {
		t.Fatalf("certificate not before: got %s, want %s", got, want)
	}
	if got, want := cert.Leaf.NotAfter.UTC().Format(time.RFC3339), vectorCertNotAfter; got != want {
		t.Fatalf("certificate not after: got %s, want %s", got, want)
	}
}
