package ports

import "context"

// Service identifies a service announced and browsed on the local network.
type Service string

// Discoverable services.
const (
	// ServiceMain is a running node that accepts connections.
	ServiceMain Service = "main"
	// ServicePairing is a node that is open for pairing.
	ServicePairing Service = "pairing"
	// ServiceFriends is a node that looks for Friends devices for a limited
	// time. Its tags come from the keys of the pairs, not from the Personal
	// group.
	ServiceFriends Service = "friends"
)

// Candidate is a discovered service instance.
type Candidate struct {
	// Addr is the host and port of the instance.
	Addr string
	// TXT is the instance metadata.
	TXT map[string]string
}

// Discovery advertises and browses services on the local network.
type Discovery interface {
	// Advertise announces a service instance on port with the given TXT records.
	// Advertising the same service again replaces the previous announcement.
	Advertise(ctx context.Context, service Service, port int, txt map[string]string) error
	// Browse delivers discovered instances of service until Stop is called or ctx is done.
	// The channel is closed when browsing stops.
	Browse(ctx context.Context, service Service) (<-chan Candidate, error)
	// Stop stops every advertisement and closes every browse channel.
	Stop() error
}
