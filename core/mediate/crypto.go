package mediate

import (
	"crypto/ecdh"
	"crypto/mlkem"
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
)

// Signature types of the transcripts.
const (
	// OfferSignatureType is the type of the signature of the mediator.
	OfferSignatureType = "mediated-pair-offer"
	// CommitSignatureType is the type of the signature of the lower endpoint.
	CommitSignatureType = "mediated-pair-commit"
	// FinishSignatureType is the type of the signature of the higher endpoint.
	FinishSignatureType = "mediated-pair-finish"
	// rendezvousLabel is the hash prefix of the rendezvous key.
	rendezvousLabel = "unison/1 mediated-rendezvous"
	// sasLabel is the hash prefix of the short authentication string.
	sasLabel = "unison/1 mediated-sas"
)

// offerTranscript builds the canonical transcript the mediator signs. Both
// offers of one exchange carry the same transcript, so the mediator signs it
// once.
func offerTranscript(sessionID [sessionIDSize]byte, mediator, low, high model.DeviceID, role model.Role, expiresAtMillis int64) []byte {
	out := make([]byte, 0, 1+sessionIDSize+3*len(mediator)+1+8)
	out = append(out, transcriptVersion)
	out = append(out, sessionID[:]...)
	out = append(out, mediator[:]...)
	out = append(out, low[:]...)
	out = append(out, high[:]...)
	out = append(out, roleByte(role))
	out = binary.BigEndian.AppendUint64(out, uint64(expiresAtMillis))
	return out
}

// commitTranscript appends the commit fields to the offer transcript.
func commitTranscript(base, x25519Pub, mlkemEK []byte, platform model.Platform, name string, addrs []string) []byte {
	out := append([]byte(nil), base...)
	out = append(out, x25519Pub...)
	out = append(out, mlkemEK...)
	out = append(out, platformByte(platform))
	out = appendString(out, name)
	return appendAddrs(out, addrs)
}

// finishTranscript appends the finish fields and repeats the ephemeral keys of
// the lower endpoint, so the mediator cannot replace them on the way.
func finishTranscript(base, x25519Low, mlkemEKLow, x25519High, mlkemCT []byte, platform model.Platform, name string, addrs []string) []byte {
	out := append([]byte(nil), base...)
	out = append(out, x25519Low...)
	out = append(out, mlkemEKLow...)
	out = append(out, x25519High...)
	out = append(out, mlkemCT...)
	out = append(out, platformByte(platform))
	out = appendString(out, name)
	return appendAddrs(out, addrs)
}

// appendString appends a length prefixed string.
func appendString(out []byte, value string) []byte {
	out = binary.BigEndian.AppendUint16(out, uint16(len(value)))
	return append(out, value...)
}

// appendAddrs appends the count and the length prefixed addresses.
func appendAddrs(out []byte, addrs []string) []byte {
	out = binary.BigEndian.AppendUint16(out, uint16(len(addrs)))
	for _, addr := range addrs {
		out = appendString(out, addr)
	}
	return out
}

// deriveRendezvousKey computes the rendezvous key of the pair. Both endpoints
// derive the same key from the two shared secrets and the full transcript. The
// mediator sees the transcript but neither shared secret.
func deriveRendezvousKey(base, x25519Low, mlkemEKLow, x25519High, mlkemCT, sharedX25519, sharedMLKEM []byte) []byte {
	hash := blake3.New(keySize, nil)
	_, _ = hash.Write([]byte(rendezvousLabel))
	_, _ = hash.Write(base)
	_, _ = hash.Write(x25519Low)
	_, _ = hash.Write(mlkemEKLow)
	_, _ = hash.Write(x25519High)
	_, _ = hash.Write(mlkemCT)
	_, _ = hash.Write(sharedX25519)
	_, _ = hash.Write(sharedMLKEM)
	return hash.Sum(nil)
}

// deriveSAS computes the verification code both endpoints show.
func deriveSAS(sessionID [sessionIDSize]byte, mediator model.DeviceID, low, high identity.PublicIdentity, role model.Role) pairing.SAS {
	hash := blake3.New(keySize, nil)
	_, _ = hash.Write([]byte(sasLabel))
	_, _ = hash.Write([]byte{transcriptVersion})
	_, _ = hash.Write(sessionID[:])
	_, _ = hash.Write(mediator[:])
	_, _ = hash.Write(low.Ed25519)
	_, _ = hash.Write(low.MLDSABytes())
	_, _ = hash.Write(high.Ed25519)
	_, _ = hash.Write(high.MLDSABytes())
	_, _ = hash.Write([]byte{roleByte(role)})
	return pairing.SASFromSeed(hash.Sum(nil))
}

// ephemeralKeys is the key material of the lower endpoint.
type ephemeralKeys struct {
	x25519 *ecdh.PrivateKey
	mlkem  *mlkem.DecapsulationKey768
	xPub   []byte
	ek     []byte
}

// newEphemeralKeys generates the ephemeral X25519 and ML-KEM-768 pairs of the
// lower endpoint.
func newEphemeralKeys() (*ephemeralKeys, error) {
	x25519, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("mediate: generate x25519: %w", err)
	}
	decap, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, fmt.Errorf("mediate: generate ml-kem: %w", err)
	}
	keys := &ephemeralKeys{
		x25519: x25519,
		mlkem:  decap,
		xPub:   x25519.PublicKey().Bytes(),
		ek:     decap.EncapsulationKey().Bytes(),
	}
	return keys, nil
}

// destroy wipes the private key material.
func (k *ephemeralKeys) destroy() {
	if k == nil {
		return
	}
	if k.xPub != nil {
		clear(k.xPub)
	}
	if k.ek != nil {
		clear(k.ek)
	}
	k.x25519 = nil
	k.mlkem = nil
}

// highShare is the shared secret of the higher endpoint.
type highShare struct {
	x25519   *ecdh.PrivateKey
	xPub     []byte
	mlkemCT  []byte
	sharedX  []byte
	sharedPQ []byte
}

// encapsulate generates the ephemeral X25519 key of the higher endpoint and
// encapsulates to the ML-KEM key of the lower endpoint.
func encapsulate(lowX25519, lowEK []byte) (*highShare, error) {
	x25519, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("mediate: generate x25519: %w", err)
	}
	ek, err := mlkem.NewEncapsulationKey768(lowEK)
	if err != nil {
		return nil, fmt.Errorf("mediate: ml-kem encapsulation key: %w", err)
	}
	sharedPQ, ct := ek.Encapsulate()
	pub, err := ecdh.X25519().NewPublicKey(lowX25519)
	if err != nil {
		return nil, fmt.Errorf("mediate: x25519 public key: %w", err)
	}
	sharedX, err := x25519.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("mediate: x25519 shared secret: %w", err)
	}
	return &highShare{
		x25519:   x25519,
		xPub:     x25519.PublicKey().Bytes(),
		mlkemCT:  ct,
		sharedX:  sharedX,
		sharedPQ: sharedPQ,
	}, nil
}

// destroy wipes the private key material and the shared secrets.
func (h *highShare) destroy() {
	if h == nil {
		return
	}
	clear(h.xPub)
	clear(h.mlkemCT)
	clear(h.sharedX)
	clear(h.sharedPQ)
	h.x25519 = nil
}

// decapsulate derives the shared secrets of the lower endpoint from the finish
// message.
func decapsulate(low *ephemeralKeys, x25519High, mlkemCT []byte) (sharedX, sharedPQ []byte, err error) {
	pub, err := ecdh.X25519().NewPublicKey(x25519High)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: x25519 public key: %w", ErrInvalidFinish, err)
	}
	sharedX, err = low.x25519.ECDH(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: x25519 shared secret: %w", ErrInvalidFinish, err)
	}
	sharedPQ, err = low.mlkem.Decapsulate(mlkemCT)
	if err != nil {
		clear(sharedX)
		return nil, nil, fmt.Errorf("%w: ml-kem ciphertext: %w", ErrInvalidFinish, err)
	}
	return sharedX, sharedPQ, nil
}

// verifySignature verifies a hybrid signature with a clear error.
func verifySignature(public identity.PublicIdentity, typ string, msg []byte, sig identity.Signature) error {
	if len(sig.Ed25519) == 0 || len(sig.MLDSA) == 0 {
		return identity.ErrInvalidSignature
	}
	return public.Verify(typ, msg, sig)
}

// validSessionID checks the length of an exchange identifier.
func validSessionID(id []byte) bool {
	return len(id) == sessionIDSize
}

// sessionIDString formats an exchange identifier for the API and for logs.
func sessionIDString(id []byte) string {
	return fmt.Sprintf("%x", id)
}
