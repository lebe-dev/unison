package mobile

import "crypto/ed25519"

// releaseSignatureHead separates the signature of a release manifest from any
// other use of the same key.
const releaseSignatureHead = "unison/1 release\x00"

// VerifyReleaseSignature reports whether sig is a valid Ed25519 signature over
// "unison/1 release\x00" || payload under the 32 byte public key pub. It returns
// false for a key or a signature of another size.
func VerifyReleaseSignature(pub []byte, payload []byte, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	message := make([]byte, 0, len(releaseSignatureHead)+len(payload))
	message = append(message, releaseSignatureHead...)
	message = append(message, payload...)
	return ed25519.Verify(ed25519.PublicKey(pub), message, sig)
}
