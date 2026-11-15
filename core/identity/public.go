package identity

import (
	"crypto/ed25519"
	"crypto/mldsa"
	"errors"
	"fmt"
	"slices"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
)

// deviceIDSize is the length of a device ID in bytes.
const deviceIDSize = len(model.DeviceID{})

// PublicIdentity is the public half of a device identity.
type PublicIdentity struct {
	// DeviceID is the ID derived from the public keys.
	DeviceID model.DeviceID
	// Ed25519 is the Ed25519 identity public key.
	Ed25519 ed25519.PublicKey
	// MLDSA65 is the ML-DSA-65 identity public key.
	MLDSA65 *mldsa.PublicKey
}

// NewPublicIdentity validates the encoded public keys and derives the device ID.
func NewPublicIdentity(edPub ed25519.PublicKey, mldsaPub []byte) (PublicIdentity, error) {
	var public PublicIdentity
	if len(edPub) != ed25519.PublicKeySize {
		return public, fmt.Errorf("identity: ed25519 public key: got %d bytes, want %d", len(edPub), ed25519.PublicKeySize)
	}
	pqPub, err := mldsa.NewPublicKey(mldsa.MLDSA65(), mldsaPub)
	if err != nil {
		return public, fmt.Errorf("identity: mldsa public key: %w", err)
	}

	public.DeviceID = DeviceIDFromKeys(edPub, mldsaPub)
	public.Ed25519 = slices.Clone(edPub)
	public.MLDSA65 = pqPub
	return public, nil
}

// DeviceIDFromKeys derives a device ID from the identity public keys.
func DeviceIDFromKeys(edPub ed25519.PublicKey, mldsaPub []byte) model.DeviceID {
	keyed := make([]byte, 0, len(edPub)+len(mldsaPub))
	keyed = append(keyed, edPub...)
	keyed = append(keyed, mldsaPub...)
	return model.DeviceID(blake3.Sum256(keyed))
}

// Fingerprint returns the human readable fingerprint of the device ID.
func (p PublicIdentity) Fingerprint() model.Fingerprint {
	return model.FingerprintOf(p.DeviceID)
}

// MLDSABytes returns the encoded ML-DSA-65 public key.
func (p PublicIdentity) MLDSABytes() []byte {
	if p.MLDSA65 == nil {
		return nil
	}
	return p.MLDSA65.Bytes()
}

// Encoding of a public identity: version byte, device ID, Ed25519 public key and
// ML-DSA-65 public key.
const (
	publicIdentityVersion = 1
	publicIdentitySize    = 1 + deviceIDSize + ed25519.PublicKeySize + mldsa.MLDSA65PublicKeySize
)

// MarshalBinary encodes the public identity for the wire.
func (p PublicIdentity) MarshalBinary() ([]byte, error) {
	if len(p.Ed25519) != ed25519.PublicKeySize || p.MLDSA65 == nil {
		return nil, errors.New("identity: incomplete public identity")
	}

	out := make([]byte, 0, publicIdentitySize)
	out = append(out, publicIdentityVersion)
	out = append(out, p.DeviceID[:]...)
	out = append(out, p.Ed25519...)
	out = append(out, p.MLDSA65.Bytes()...)
	return out, nil
}

// UnmarshalPublicIdentity decodes a public identity and checks that the device ID
// matches the public keys.
func UnmarshalPublicIdentity(data []byte) (PublicIdentity, error) {
	var public PublicIdentity
	if len(data) != publicIdentitySize {
		return public, fmt.Errorf("identity: public identity: got %d bytes, want %d", len(data), publicIdentitySize)
	}
	if data[0] != publicIdentityVersion {
		return public, fmt.Errorf("identity: public identity: unknown version %d", data[0])
	}

	offset := 1
	id, err := model.DeviceIDFromBytes(data[offset : offset+deviceIDSize])
	if err != nil {
		return public, fmt.Errorf("identity: public identity: %w", err)
	}
	offset += deviceIDSize

	edPub := data[offset : offset+ed25519.PublicKeySize]
	offset += ed25519.PublicKeySize

	public, err = NewPublicIdentity(edPub, data[offset:])
	if err != nil {
		return PublicIdentity{}, err
	}
	if public.DeviceID != id {
		return PublicIdentity{}, errors.New("identity: public identity: device id does not match the keys")
	}
	return public, nil
}
