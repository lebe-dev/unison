// Package gossip spreads the trust state of the Personal group over the
// sessions between its devices: the introductions of new devices and the group
// data (the discovery key and the relay access).
//
// Both devices of a session send a digest of what they know, then each one sends
// the records the other one lacks. A device applies only records that belong to
// a trusted, non-revoked Personal device with a valid signature.
package gossip
