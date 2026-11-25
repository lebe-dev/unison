package pairing

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/identity"
)

// Parameters of the short authentication string.
const (
	// sasLabel is the TLS exporter label and the hash prefix of the SAS.
	sasLabel = "unison/1 sas"
	// sasKeySize is the length of the exporter key material in bytes.
	sasKeySize = 32
	// sasDigitCount is the number of decimal digits shown to the user.
	sasDigitCount = 6
	// sasEmojiCount is the number of emoji shown to the user.
	sasEmojiCount = 4
	// sasModulus is the range of the decimal part.
	sasModulus = 1_000_000
)

// emojiTable is the fixed alphabet of the emoji part of a SAS. It has 64
// entries, so a digest byte picks one without bias.
var emojiTable = [64]string{
	"🐶", "🐱", "🐭", "🐹", "🐰", "🦊", "🐻", "🐼",
	"🐨", "🐯", "🦁", "🐮", "🐷", "🐸", "🐵", "🐔",
	"🐧", "🐦", "🦆", "🦅", "🦉", "🦇", "🐺", "🐗",
	"🐴", "🦄", "🐝", "🐛", "🦋", "🐌", "🐞", "🐢",
	"🐍", "🦎", "🐙", "🦑", "🦀", "🐟", "🐬", "🐳",
	"🦈", "🐊", "🐘", "🦒", "🦓", "🐄", "🐑", "🐐",
	"🐓", "🦃", "🦜", "🐇", "🦔", "🐁", "🐀", "🐿",
	"🌵", "🌲", "🌳", "🌴", "🌱", "🌿", "🍀", "🍁",
}

// SAS is the short authentication string two devices show while pairing. The
// user compares it on both screens and confirms the match.
type SAS struct {
	// Digits are six decimal digits.
	Digits string
	// Emoji are four emoji from a fixed table.
	Emoji []string
}

// String returns the SAS as it is shown to the user.
func (s SAS) String() string {
	return strings.Join(s.Emoji, "") + " " + s.Digits
}

// IsZero reports whether the SAS is empty.
func (s SAS) IsZero() bool {
	return s.Digits == ""
}

// computeSAS derives the SAS of a pairing session from the exporter key
// material of the TLS session and the two public identities. The identities are
// ordered by device ID, so both devices hash the same input.
func computeSAS(ekm []byte, one, other identity.PublicIdentity) (SAS, error) {
	if len(ekm) != sasKeySize {
		return SAS{}, fmt.Errorf("pairing: exporter key material: got %d bytes, want %d", len(ekm), sasKeySize)
	}

	first, second := orderIdentities(one, other)
	firstBytes, err := first.MarshalBinary()
	if err != nil {
		return SAS{}, fmt.Errorf("pairing: %w", err)
	}
	secondBytes, err := second.MarshalBinary()
	if err != nil {
		return SAS{}, fmt.Errorf("pairing: %w", err)
	}

	hash := blake3.New(sasKeySize, nil)
	_, _ = hash.Write([]byte(sasLabel))
	_, _ = hash.Write(ekm)
	_, _ = hash.Write(firstBytes)
	_, _ = hash.Write(secondBytes)
	return SASFromSeed(hash.Sum(nil)), nil
}

// SASFromSeed derives a SAS from a 32-byte seed. Every device that derives the
// same seed shows the same code, so a protocol with another handshake uses it
// instead of the pairing key material.
func SASFromSeed(seed []byte) SAS {
	if len(seed) < 8+sasEmojiCount {
		return SAS{}
	}
	digits := binary.BigEndian.Uint64(seed[:8]) % sasModulus
	emoji := make([]string, 0, sasEmojiCount)
	for _, b := range seed[8 : 8+sasEmojiCount] {
		emoji = append(emoji, emojiTable[b&63])
	}
	return SAS{Digits: fmt.Sprintf("%06d", digits), Emoji: emoji}
}

// orderIdentities returns two identities ordered by device ID.
func orderIdentities(one, other identity.PublicIdentity) (identity.PublicIdentity, identity.PublicIdentity) {
	if bytes.Compare(one.DeviceID[:], other.DeviceID[:]) <= 0 {
		return one, other
	}
	return other, one
}
