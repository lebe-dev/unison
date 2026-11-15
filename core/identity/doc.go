// Package identity manages the device key pair, its device ID, hybrid signatures
// and the self-signed certificate used for mutual TLS.
//
// Keys are Ed25519 and ML-DSA-65. Signature types separate the domains of
// different messages and are short lowercase words such as "pair-request".
// Hybrid signatures carry both parts and a peer accepts them only when both
// verify; a post-quantum only signature is available for messages the protocol
// signs with the ML-DSA key alone.
package identity
