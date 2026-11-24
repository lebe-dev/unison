package nodetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/mediate"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
)

// waitIncoming waits until the node holds an incoming exchange with the
// mediator.
func waitIncoming(t *testing.T, n *Node, mediator *Node) mediate.Exchange {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, exchange := range n.MediatedPairs() {
			if exchange.Direction != mediate.DirectionIncoming {
				continue
			}
			if exchange.Mediator.ID != mediator.DeviceID() {
				continue
			}
			return exchange
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s received no offer from %s", n.Name(), mediator.Name())
	return mediate.Exchange{}
}

// waitTrustedBy waits until the node stores the device with the role.
func waitTrustedBy(t *testing.T, n *Node, subject *Node, role model.Role) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		peer, err := n.Trust().Get(context.Background(), subject.DeviceID())
		if err == nil {
			if peer.Role != role {
				t.Fatalf("%s stores %s as %s, want %s", n.Name(), subject.Name(), peer.Role, role)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s does not trust %s", n.Name(), subject.Name())
}

// mediatePair introduces first and second through the mediator. confirmFirst
// selects which user accepts before the other one.
func mediatePair(t *testing.T, mediator, first, second *Node, role model.Role, confirmFirst bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := mediator.StartMediatedPair(ctx, first.DeviceID(), second.DeviceID(), role); err != nil {
		t.Fatalf("start the exchange: %v", err)
	}

	offerFirst := waitIncoming(t, first, mediator)
	offerSecond := waitIncoming(t, second, mediator)
	if offerFirst.SAS.String() != offerSecond.SAS.String() {
		t.Fatalf("the verification codes differ: %s and %s", offerFirst.SAS, offerSecond.SAS)
	}
	if offerFirst.Role != role {
		t.Fatalf("the offered role is %s, want %s", offerFirst.Role, role)
	}

	enable := role == model.RoleFriends
	early, late := first, second
	earlyOffer, lateOffer := offerFirst, offerSecond
	if !confirmFirst {
		early, late = second, first
		earlyOffer, lateOffer = offerSecond, offerFirst
	}
	if err := early.ConfirmMediatedPair(ctx, earlyOffer.SessionID, enable); err != nil {
		t.Fatalf("%s confirms: %v", early.Name(), err)
	}
	if err := late.ConfirmMediatedPair(ctx, lateOffer.SessionID, enable); err != nil {
		t.Fatalf("%s confirms: %v", late.Name(), err)
	}

	waitTrustedBy(t, first, second, role)
	waitTrustedBy(t, second, first, role)

	peer, err := first.Trust().Get(context.Background(), second.DeviceID())
	if err != nil {
		t.Fatalf("read the new peer: %v", err)
	}
	if peer.IntroducedBy != mediator.DeviceID() {
		t.Fatalf("the peer was introduced by %s, want %s", peer.IntroducedBy, mediator.DeviceID())
	}

	first.WaitOnline(second)
	second.WaitOnline(first)
}

// TestMediatedPairPersonal introduces two devices of a Personal group.
func TestMediatedPairPersonal(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	gamma := h.NewNode("gamma")

	h.Pair(alpha, beta)
	h.Pair(beta, gamma)

	mediatePair(t, beta, alpha, gamma, model.RolePersonal, true)
}

// TestMediatedPairFriends introduces two devices as Friends, with the higher
// endpoint confirming first.
func TestMediatedPairFriends(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	gamma := h.NewNode("gamma")

	h.Pair(alpha, beta)
	h.Pair(beta, gamma)

	mediatePair(t, beta, alpha, gamma, model.RoleFriends, false)
}

// pairFriends stores the devices as Friends peers and connects them on a known
// address, which the search for Friends devices allows.
func pairFriends(t *testing.T, one, other *Node) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pair := range [][2]*Node{{one, other}, {other, one}} {
		from, to := pair[0], pair[1]
		if _, err := from.SetFriendsDiscovery(ctx, true); err != nil {
			t.Fatalf("%s: friends discovery: %v", from.Name(), err)
		}
		if err := from.AddPeer(ctx, trust.Peer{
			Public:   to.Identity().Public(),
			Name:     to.name,
			Platform: to.platform,
			Role:     model.RoleFriends,
		}); err != nil {
			t.Fatalf("%s: add peer: %v", from.Name(), err)
		}
		if err := from.SetPeerAddress(ctx, to.DeviceID(), to.ListenAddr()); err != nil {
			t.Fatalf("%s: set address: %v", from.Name(), err)
		}
	}
	one.WaitOnline(other)
	other.WaitOnline(one)
}

// TestMediatedPairRefused checks that a Friends mediator cannot propose a
// Personal pair.
func TestMediatedPairRefused(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	gamma := h.NewNode("gamma")

	pairFriends(t, alpha, beta)
	h.Pair(beta, gamma)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := beta.StartMediatedPair(ctx, alpha.DeviceID(), gamma.DeviceID(), model.RolePersonal)
	if !errors.Is(err, mediate.ErrRoleEscalation) {
		t.Fatalf("the exchange started: %v", err)
	}
}
