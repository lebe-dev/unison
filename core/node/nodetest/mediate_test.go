package nodetest

import (
	"bytes"
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

// TestMediatedPairLowConfirmsFirstKeepsHighWaiting checks that when the
// endpoint with the lower device ID confirms first, the higher endpoint keeps
// StateWaiting until its own user decides, so UIs and notifications remain
// visible and actionable.
func TestMediatedPairLowConfirmsFirstKeepsHighWaiting(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	gamma := h.NewNode("gamma")

	h.Pair(alpha, beta)
	h.Pair(beta, gamma)

	low, high := alpha, gamma
	alphaID, gammaID := alpha.DeviceID(), gamma.DeviceID()
	if bytes.Compare(alphaID[:], gammaID[:]) > 0 {
		low, high = gamma, alpha
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := beta.StartMediatedPair(ctx, low.DeviceID(), high.DeviceID(), model.RolePersonal); err != nil {
		t.Fatalf("start the exchange: %v", err)
	}

	offerLow := waitIncoming(t, low, beta)
	offerHigh := waitIncoming(t, high, beta)

	if err := low.ConfirmMediatedPair(ctx, offerLow.SessionID, false); err != nil {
		t.Fatalf("low confirms: %v", err)
	}

	// Wait until the mediator reaches StateAwaitingFinish (commit forwarded to
	// high), then verify high still reports StateWaiting.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pairs := beta.MediatedPairs()
		if len(pairs) == 1 && pairs[0].State == mediate.StateAwaitingFinish {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	highPairs := high.MediatedPairs()
	if len(highPairs) != 1 {
		t.Fatalf("high has %d exchanges, want 1", len(highPairs))
	}
	if highPairs[0].State != mediate.StateWaiting {
		t.Fatalf("high state = %s, want %s while local user has not decided", highPairs[0].State, mediate.StateWaiting)
	}

	if err := high.ConfirmMediatedPair(ctx, offerHigh.SessionID, false); err != nil {
		t.Fatalf("high confirms: %v", err)
	}

	waitTrustedBy(t, low, high, model.RolePersonal)
	waitTrustedBy(t, high, low, model.RolePersonal)
	low.WaitOnline(high)
	high.WaitOnline(low)
}

// TestMediatedPairRejectAndCancel checks that declining or canceling an
// introduction ends the exchange on all three devices without writing to the
// trust stores.
func TestMediatedPairRejectAndCancel(t *testing.T) {
	h := New(t)
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	gamma := h.NewNode("gamma")

	h.Pair(alpha, beta)
	h.Pair(beta, gamma)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Case 1: one endpoint rejects.
	if _, err := beta.StartMediatedPair(ctx, alpha.DeviceID(), gamma.DeviceID(), model.RolePersonal); err != nil {
		t.Fatalf("start exchange 1: %v", err)
	}
	offerAlpha := waitIncoming(t, alpha, beta)
	_ = waitIncoming(t, gamma, beta)

	if err := alpha.RejectMediatedPair(ctx, offerAlpha.SessionID); err != nil {
		t.Fatalf("alpha rejects: %v", err)
	}
	waitNoMediatedPairs(t, alpha)
	waitNoMediatedPairs(t, beta)
	waitNoMediatedPairs(t, gamma)

	// Case 2: mediator cancels.
	started, err := beta.StartMediatedPair(ctx, alpha.DeviceID(), gamma.DeviceID(), model.RolePersonal)
	if err != nil {
		t.Fatalf("start exchange 2: %v", err)
	}
	_ = waitIncoming(t, alpha, beta)
	_ = waitIncoming(t, gamma, beta)

	if err := beta.CancelMediatedPair(ctx, started.SessionID); err != nil {
		t.Fatalf("beta cancels: %v", err)
	}
	waitNoMediatedPairs(t, alpha)
	waitNoMediatedPairs(t, beta)
	waitNoMediatedPairs(t, gamma)

	if _, err := alpha.Trust().Get(ctx, gamma.DeviceID()); !errors.Is(err, trust.ErrPeerNotFound) {
		t.Fatalf("alpha stored gamma after rejection/cancel: %v", err)
	}
	if _, err := gamma.Trust().Get(ctx, alpha.DeviceID()); !errors.Is(err, trust.ErrPeerNotFound) {
		t.Fatalf("gamma stored alpha after rejection/cancel: %v", err)
	}
}

func waitNoMediatedPairs(t *testing.T, n *Node) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(n.MediatedPairs()) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s still holds %d mediated pairs", n.Name(), len(n.MediatedPairs()))
}
