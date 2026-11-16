package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
)

// Errors reported by the QR token.
var (
	// ErrToken reports a pairing token that the window does not accept any more.
	ErrToken = errors.New("pairing: pairing token is not accepted")
)

// tokenSize is the length of a pairing token in bytes.
const tokenSize = 16

// Token is the one time token of a pairing QR code. The device that shows the
// code puts the token into the code, and the device that scans it sends the
// token back to prove that it saw the code.
type Token [tokenSize]byte

// NewToken returns a random token.
func NewToken() (Token, error) {
	var token Token
	if _, err := rand.Read(token[:]); err != nil {
		return Token{}, fmt.Errorf("pairing: generate token: %w", err)
	}
	return token, nil
}

// String returns the lowercase hex form of the token.
func (t Token) String() string {
	return hex.EncodeToString(t[:])
}

// IsZero reports whether the token is empty.
func (t Token) IsZero() bool {
	return t == Token{}
}

// ParseToken parses a hex encoded token of exactly 16 bytes.
func ParseToken(s string) (Token, error) {
	var token Token
	if !isLowerHex(s, tokenSize*2) {
		return token, fmt.Errorf("%w: token of %d characters, want %d lowercase hex", ErrQR, len(s), tokenSize*2)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return token, fmt.Errorf("%w: token: %w", ErrQR, err)
	}
	copy(token[:], b)
	return token, nil
}

// tokenMatches reports whether b is the token, in constant time.
func (t Token) tokenMatches(b []byte) bool {
	if len(b) != tokenSize {
		return false
	}
	return subtle.ConstantTimeCompare(t[:], b) == 1
}
