package trust

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzIntroduction checks that an arbitrary introduction record is either
// refused or describes a consistent device.
func FuzzIntroduction(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x08, 0x20})
	// Keys of the wrong length are refused before the identity is built.
	f.Add([]byte{0x0a, 0x04, 0x01, 0x02, 0x03, 0x04, 0x12, 0x04, 0x05, 0x06, 0x07, 0x08})

	f.Fuzz(func(t *testing.T, data []byte) {
		message := &wirev1.Introduction{}
		if err := proto.Unmarshal(data, message); err != nil {
			return
		}
		intro, err := IntroductionFromProto(message)
		if err != nil {
			return
		}
		if intro.Role != model.RolePersonal {
			t.Fatalf("accepted an introduction with role %s", intro.Role)
		}
		if intro.Subject.DeviceID.IsZero() || intro.Issuer.IsZero() {
			t.Fatalf("accepted an introduction without a subject or an issuer")
		}
		if intro.Subject.DeviceID == intro.Issuer {
			t.Fatalf("accepted an introduction of the issuer")
		}
		if intro.IssuedAt.IsZero() {
			t.Fatalf("accepted an introduction without a time")
		}
	})
}

// FuzzRevocation checks that an arbitrary revocation record is either refused
// or describes a consistent device.
func FuzzRevocation(f *testing.F) {
	seed := &wirev1.Revocation{
		Subject:  make([]byte, 32),
		Issuer:   make([]byte, 32),
		TsUnixMs: 1763000000000,
	}
	seed.Subject[0], seed.Issuer[0] = 1, 2
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x01, 0x01})

	f.Fuzz(func(t *testing.T, data []byte) {
		message := &wirev1.Revocation{}
		if err := proto.Unmarshal(data, message); err != nil {
			return
		}
		revocation, err := RevocationFromProto(message)
		if err != nil {
			return
		}
		if revocation.Subject.IsZero() || revocation.Issuer.IsZero() {
			t.Fatalf("accepted a revocation without a subject or an issuer")
		}
		if revocation.IssuedAt.IsZero() {
			t.Fatalf("accepted a revocation without a time")
		}
	})
}

// FuzzSetPeerPolicy checks that an arbitrary policy record is either refused or
// targets a device.
func FuzzSetPeerPolicy(f *testing.F) {
	seed := &wirev1.SetPeerPolicy{
		Target:              make([]byte, 32),
		RequireConfirmation: true,
		Scope:               wirev1.ConfirmationScope_CONFIRMATION_SCOPE_FILES,
		TsUnixMs:            1763000000000,
	}
	seed.Target[0] = 1
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x20})

	f.Fuzz(func(t *testing.T, data []byte) {
		message := &wirev1.SetPeerPolicy{}
		if err := proto.Unmarshal(data, message); err != nil {
			return
		}
		policy, err := SetPeerPolicyFromProto(message)
		if err != nil {
			return
		}
		if policy.Target.IsZero() {
			t.Fatalf("accepted a policy without a target")
		}
		if policy.SignedAt.IsZero() {
			t.Fatalf("accepted a policy without a time")
		}
	})
}
