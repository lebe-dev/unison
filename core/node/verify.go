package node

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/trust"
)

// peerVerifier accepts the devices of the trust store and refuses every other
// one. The transport calls it during the handshake and again after it.
type peerVerifier struct {
	trust *trust.Store
	// friendsOn reports whether the device looks for Friends devices. Friends
	// connections are refused while it does not.
	friendsOn func() bool
}

var _ transport.PeerVerifier = peerVerifier{}

// VerifyPeer returns the peer that owns the certificate. An unknown or revoked
// device is refused.
func (v peerVerifier) VerifyPeer(ctx context.Context, cert *x509.Certificate) (transport.Peer, error) {
	if cert == nil {
		return transport.Peer{}, errors.New("node: no peer certificate")
	}
	key, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return transport.Peer{}, errors.New("node: peer certificate is not ed25519")
	}

	peer, err := v.trust.ByEd25519(ctx, key)
	if err != nil {
		return transport.Peer{}, err
	}
	if peer.Revoked {
		return transport.Peer{}, trust.ErrPeerRevoked
	}
	if peer.Role == model.RoleFriends && (v.friendsOn == nil || !v.friendsOn()) {
		return transport.Peer{}, ErrFriendsDiscoveryOff
	}
	return transport.Peer{Public: peer.Public, Role: peer.Role}, nil
}
