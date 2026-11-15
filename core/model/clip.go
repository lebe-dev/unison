package model

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// ClipID is the UUIDv7 identifier of a clip item.
type ClipID [16]byte

// String returns the canonical UUID form of the ID.
func (c ClipID) String() string {
	h := hex.EncodeToString(c[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// Bytes returns a copy of the raw ID.
func (c ClipID) Bytes() []byte {
	return c[:]
}

// IsZero reports whether the ID is all zeroes.
func (c ClipID) IsZero() bool {
	return c == ClipID{}
}

// ParseClipID parses a UUID with or without dashes.
func ParseClipID(s string) (ClipID, error) {
	var id ClipID
	h := strings.ReplaceAll(s, "-", "")
	if len(h) != 32 {
		return id, fmt.Errorf("parse clip id: got %d hex characters, want 32", len(h))
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		return id, fmt.Errorf("parse clip id: %w", err)
	}
	copy(id[:], b)
	return id, nil
}

// Representation is one MIME representation of clip content.
type Representation struct {
	// MIME is the content type, for example "text/plain".
	MIME string
	// Size is the payload size in bytes.
	Size uint64
	// Hash is the BLAKE3 hash of the payload.
	Hash [32]byte
	// Inline carries the payload when it is small enough to travel with the item.
	Inline []byte
	// FileName is set for file representations.
	FileName string
	// MTime is the file modification time, zero for other representations.
	MTime time.Time
}

// ClipItem is one clipboard history entry.
type ClipItem struct {
	// ID is the unique identifier of the item.
	ID ClipID
	// Origin is the device that captured the item.
	Origin DeviceID
	// Seq is a monotonic per-origin counter.
	Seq uint64
	// CreatedAt is the capture time.
	CreatedAt time.Time
	// ExpiresAt is the expiry time set by the origin device.
	ExpiresAt time.Time
	// Kind is the content kind.
	Kind Kind
	// Representations holds every payload representation of the item.
	Representations []Representation
	// Source is where the item was captured.
	Source Source
	// Deleted marks a tombstone of a manual deletion.
	Deleted bool
}
