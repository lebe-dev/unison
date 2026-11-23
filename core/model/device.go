package model

import (
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
)

// DeviceID identifies a device: the first 32 bytes of BLAKE3 over its identity public keys.
type DeviceID [32]byte

// String returns the lowercase hex form of the ID.
func (d DeviceID) String() string {
	return hex.EncodeToString(d[:])
}

// LogValue returns the hex form of the ID, so logs carry the ID rather than the
// bytes of the array.
func (d DeviceID) LogValue() slog.Value {
	return slog.StringValue(d.String())
}

// Bytes returns a copy of the raw ID.
func (d DeviceID) Bytes() []byte {
	return d[:]
}

// IsZero reports whether the ID is all zeroes.
func (d DeviceID) IsZero() bool {
	return d == DeviceID{}
}

// ParseDeviceID parses a hex encoded device ID.
func ParseDeviceID(s string) (DeviceID, error) {
	var id DeviceID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("parse device id: %w", err)
	}
	if len(b) != len(id) {
		return id, fmt.Errorf("parse device id: got %d bytes, want %d", len(b), len(id))
	}
	copy(id[:], b)
	return id, nil
}

// DeviceIDFromBytes copies exactly 32 bytes into a DeviceID.
func DeviceIDFromBytes(b []byte) (DeviceID, error) {
	var id DeviceID
	if len(b) != len(id) {
		return id, fmt.Errorf("device id: got %d bytes, want %d", len(b), len(id))
	}
	copy(id[:], b)
	return id, nil
}

// fingerprintBytes is the number of leading device ID bytes shown to the user.
const fingerprintBytes = 10

// Fingerprint is the short human readable form of a device ID.
type Fingerprint string

// FingerprintOf derives the fingerprint of a device ID: base32 of its first 10 bytes
// in groups of 4 characters.
func FingerprintOf(id DeviceID) Fingerprint {
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id[:fingerprintBytes])
	var b strings.Builder
	b.Grow(len(encoded) + len(encoded)/4)
	for i := 0; i < len(encoded); i += 4 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(encoded[i:min(i+4, len(encoded))])
	}
	return Fingerprint(b.String())
}
