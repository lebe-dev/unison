package identity

import (
	"bytes"
	"testing"
)

// FuzzPublicIdentity feeds arbitrary bytes to the decoder of a public
// identity, which arrives from peers during pairing and gossip. A decoded
// identity must encode back to the same bytes, so one identity has exactly one
// form, and its device ID must follow from its keys.
func FuzzPublicIdentity(f *testing.F) {
	id, err := FromSeeds(testSeeds())
	if err != nil {
		f.Fatalf("from seeds: %v", err)
	}
	valid, err := id.Public().MarshalBinary()
	if err != nil {
		f.Fatalf("marshal: %v", err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)-1])
	f.Add(append([]byte{0xff}, valid[1:]...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		public, err := UnmarshalPublicIdentity(data)
		if err != nil {
			return
		}
		again, err := public.MarshalBinary()
		if err != nil {
			t.Fatalf("a decoded identity does not encode: %v", err)
		}
		if !bytes.Equal(again, data) {
			t.Fatal("a public identity decodes from a non-canonical form")
		}
		derived, err := NewPublicIdentity(public.Ed25519, public.MLDSABytes())
		if err != nil || derived.DeviceID != public.DeviceID {
			t.Fatalf("the device id does not follow from the keys: %v", err)
		}
	})
}
