// Package trust records which devices are trusted, what each of them may do and
// which group secrets the local device shares with them.
//
// A peer record carries the device identity, the role assigned at pairing and
// the local policy for that device. Revoked devices stay in the store as
// deny-list entries. Fields needed to look a peer up stay in plaintext, the
// rest is sealed with the store sealer.
//
// The capability matrix is a pure function of a peer record. Callers ask it
// before acting on anything a peer sends.
package trust
