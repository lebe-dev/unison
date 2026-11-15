// Package transport carries peer sessions over QUIC with mutual TLS.
//
// Both sides present the self-signed certificate of their device identity over
// TLS 1.3 with the hybrid X25519MLKEM768 key exchange. A certificate is
// accepted only when the peer verifier trusts its key, and a connection whose
// apparently completed handshake did not use the hybrid exchange is closed.
// The first stream of a connection carries the authentication proof, which
// binds the identity key to the TLS exporter secret, to the role and to the
// device ID.
package transport
