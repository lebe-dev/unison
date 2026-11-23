package identity

import (
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"errors"
	"fmt"
)

// Errors returned when a signature type or a signature is rejected.
var (
	// ErrInvalidType reports a signature type that is not a short printable ASCII word.
	ErrInvalidType = errors.New("identity: invalid signature type")
	// ErrInvalidSignature reports a signature that failed verification.
	ErrInvalidSignature = errors.New("identity: invalid signature")
)

// contextPrefix scopes every signature context to this protocol version.
const contextPrefix = "unison/1 "

// maxTypeLength is the longest signature type that still fits the ML-DSA context,
// which is limited to 255 bytes.
const maxTypeLength = 255 - len(contextPrefix)

// Signature is a hybrid signature. A peer accepts it only when both parts verify.
type Signature struct {
	// Ed25519 is the Ed25519 signature.
	Ed25519 []byte
	// MLDSA is the ML-DSA-65 signature.
	MLDSA []byte
}

// Sign signs msg as the given type with both identity keys.
func (i *Identity) Sign(typ string, msg []byte) (Signature, error) {
	pqSig, err := i.SignMLDSA(typ, msg)
	if err != nil {
		return Signature{}, err
	}
	return Signature{
		Ed25519: ed25519.Sign(i.edKey, ed25519Input(typ, msg)),
		MLDSA:   pqSig,
	}, nil
}

// SignMLDSA signs msg as the given type with the ML-DSA-65 key only. Prefer Sign,
// this is for messages the protocol defines as post-quantum signed only.
func (i *Identity) SignMLDSA(typ string, msg []byte) ([]byte, error) {
	if err := validateType(typ); err != nil {
		return nil, err
	}
	sig, err := i.pqKey.Sign(rand.Reader, msg, &mldsa.Options{Context: mldsaContext(typ)})
	if err != nil {
		return nil, fmt.Errorf("identity: sign mldsa: %w", err)
	}
	return sig, nil
}

// Verify checks both signature parts over msg and typ.
func (p PublicIdentity) Verify(typ string, msg []byte, sig Signature) error {
	if err := validateType(typ); err != nil {
		return err
	}
	if len(p.Ed25519) != ed25519.PublicKeySize {
		return errors.New("identity: incomplete public identity")
	}
	if len(sig.Ed25519) != ed25519.SignatureSize {
		return fmt.Errorf("%w: ed25519: got %d bytes, want %d", ErrInvalidSignature, len(sig.Ed25519), ed25519.SignatureSize)
	}
	if !ed25519.Verify(p.Ed25519, ed25519Input(typ, msg), sig.Ed25519) {
		return fmt.Errorf("%w: ed25519", ErrInvalidSignature)
	}
	return p.VerifyMLDSA(typ, msg, sig.MLDSA)
}

// VerifyMLDSA checks an ML-DSA-65 only signature over msg and typ.
func (p PublicIdentity) VerifyMLDSA(typ string, msg, sig []byte) error {
	if err := validateType(typ); err != nil {
		return err
	}
	if p.MLDSA65 == nil {
		return errors.New("identity: incomplete public identity")
	}
	if len(sig) != mldsa.MLDSA65SignatureSize {
		return fmt.Errorf("%w: mldsa: got %d bytes, want %d", ErrInvalidSignature, len(sig), mldsa.MLDSA65SignatureSize)
	}
	if err := mldsa.Verify(p.MLDSA65, msg, sig, &mldsa.Options{Context: mldsaContext(typ)}); err != nil {
		return fmt.Errorf("%w: mldsa", ErrInvalidSignature)
	}
	return nil
}

// validateType checks that typ is a short printable ASCII word without spaces.
// The zero byte separator keeps the Ed25519 signing input unambiguous.
func validateType(typ string) error {
	if typ == "" || len(typ) > maxTypeLength {
		return fmt.Errorf("%w: length %d", ErrInvalidType, len(typ))
	}
	for i := 0; i < len(typ); i++ {
		if c := typ[i]; c <= ' ' || c > '~' {
			return fmt.Errorf("%w: byte 0x%02x at %d", ErrInvalidType, typ[i], i)
		}
	}
	return nil
}

// mldsaContext returns the ML-DSA context of a signature type.
func mldsaContext(typ string) string {
	return contextPrefix + typ
}

// ed25519Input returns the message Ed25519 signs. Ed25519 has no context of its
// own, so the ML-DSA context, a zero byte and the message are signed instead.
func ed25519Input(typ string, msg []byte) []byte {
	input := make([]byte, 0, len(contextPrefix)+len(typ)+1+len(msg))
	input = append(input, contextPrefix...)
	input = append(input, typ...)
	input = append(input, 0)
	input = append(input, msg...)
	return input
}
