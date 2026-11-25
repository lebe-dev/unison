package mediate

import "time"

// Limits of the mediated pairing protocol.
const (
	// Capability is the name the node announces in Hello.
	Capability = "mediated-pair/1"
	// DefaultTTL is how long an exchange waits for both users.
	DefaultTTL = 3 * time.Minute
	// MaxClockSkew bounds how far the deadline of an offer may lie in the
	// future beyond the TTL.
	MaxClockSkew = 30 * time.Second
	// MaxExchanges bounds the active exchanges of a mediator.
	MaxExchanges = 4
	// MaxPending bounds the incoming offers of one device.
	MaxPending = 4
	// MaxPendingPerMediator bounds the incoming offers of one mediator.
	MaxPendingPerMediator = 2
	// OfferRate is the interval between two offers of one mediator.
	OfferRate = 15 * time.Second
	// OfferBurst is the number of offers a mediator may send at once.
	OfferBurst = 2
	// ReplayCacheSize bounds the remembered exchange identifiers.
	ReplayCacheSize = 256
	// ReplayCacheTTL is how long an exchange identifier is remembered.
	ReplayCacheTTL = 10 * time.Minute
	// MaxAddrs bounds the address hints of one message.
	MaxAddrs = 4
	// MaxAddrLength bounds one address hint in bytes.
	MaxAddrLength = 64
	// MaxNameLength bounds a display name in bytes.
	MaxNameLength = 128
)

// Sizes of the cryptographic material.
const (
	// sessionIDSize is the length of an exchange identifier.
	sessionIDSize = 16
	// keySize is the length of a rendezvous key in bytes.
	keySize = 32
	// x25519Size is the length of an X25519 public key.
	x25519Size = 32
	// mlkemEncapsulationKeySize is the length of an ML-KEM-768 encapsulation
	// key.
	mlkemEncapsulationKeySize = 1184
	// mlkemCiphertextSize is the length of an ML-KEM-768 ciphertext.
	mlkemCiphertextSize = 1088
	// transcriptVersion is the version of the signed transcripts.
	transcriptVersion = byte(1)
)
