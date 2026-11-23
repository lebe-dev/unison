// Package protocol holds the version of the protocol that every client speaks.
// It covers the peer protocol of the sessions and the protocol of the relay.
package protocol

// Version is the current protocol version. Raise it when a change is not
// backwards compatible. A client that meets a higher peer version is outdated;
// a client that meets a lower one talks to an outdated peer.
const Version uint32 = 1

// Relation describes how the version of a peer compares to Version.
type Relation uint8

// Relations of a peer version to Version.
const (
	// Same reports the same protocol version.
	Same Relation = iota
	// PeerNewer reports a peer with a newer protocol version, so this client
	// has to be updated.
	PeerNewer
	// PeerOlder reports a peer with an older protocol version, so the peer has
	// to be updated.
	PeerOlder
)

// Compare returns how a peer version relates to Version. A missing version of
// an old relay is read as version 1.
func Compare(peer uint32) Relation {
	switch {
	case peer == Version:
		return Same
	case peer > Version:
		return PeerNewer
	default:
		return PeerOlder
	}
}

// String returns the lower case name of the relation.
func (r Relation) String() string {
	switch r {
	case PeerNewer:
		return "peer-newer"
	case PeerOlder:
		return "peer-older"
	default:
		return "same"
	}
}
