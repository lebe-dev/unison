package node

import (
	"context"
	"net"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/pairing"
)

const (
	pairDiscoveryDomain = "unison/1 pair-discovery\x00"
	pairSessionDomain   = "unison/1 pair-session\x00"
)

// pairingRelay adapts the node relay client to the pairing package.
type pairingRelay struct {
	node *Node
}

var _ pairing.Relay = (*pairingRelay)(nil)

// OpenDiscovery opens the shared pairing discovery mailbox derived from the
// group relay token.
func (r *pairingRelay) OpenDiscovery(ctx context.Context) (net.PacketConn, error) {
	token, err := r.relayToken(ctx)
	if err != nil {
		return nil, err
	}
	client, err := r.node.relayClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.Open(ctx, pairDiscoveryKey(token))
}

// OpenSession opens the per-exchange pairing mailbox derived from the group
// relay token and both ephemeral window IDs.
func (r *pairingRelay) OpenSession(ctx context.Context, initiator, responder [8]byte) (net.PacketConn, error) {
	token, err := r.relayToken(ctx)
	if err != nil {
		return nil, err
	}
	client, err := r.node.relayClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.Open(ctx, pairSessionKey(token, initiator, responder))
}

// PacketSize returns the largest QUIC packet that fits into one relay datagram.
func (r *pairingRelay) PacketSize() int {
	r.node.relayMu.Lock()
	client := r.node.relay
	r.node.relayMu.Unlock()
	if client == nil {
		return 0
	}
	return client.PacketSize()
}

// relayToken returns the configured relay access token of the group.
func (r *pairingRelay) relayToken(ctx context.Context) (string, error) {
	data, err := r.node.trust.GroupData(ctx)
	if err != nil {
		return "", err
	}
	if data.Relay.Empty() {
		return "", errRelayOff
	}
	return data.Relay.Token, nil
}

// pairDiscoveryKey derives the 32-byte mailbox key for relay pairing discovery.
func pairDiscoveryKey(token string) []byte {
	h := blake3.New(32, nil)
	_, _ = h.Write([]byte(pairDiscoveryDomain))
	_, _ = h.Write([]byte(token))
	return h.Sum(nil)
}

// pairSessionKey derives the 32-byte mailbox key for one relay pairing
// exchange.
func pairSessionKey(token string, initiator, responder [8]byte) []byte {
	h := blake3.New(32, nil)
	_, _ = h.Write([]byte(pairSessionDomain))
	_, _ = h.Write([]byte(token))
	_, _ = h.Write(initiator[:])
	_, _ = h.Write(responder[:])
	return h.Sum(nil)
}
