package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// pairThroughWindow pairs two started nodes through their pairing windows.
func pairThroughWindow(t *testing.T, initiator, responder *testNode) {
	t.Helper()

	eventsInitiator := initiator.subscribe(t)
	eventsResponder := responder.subscribe(t)
	if err := initiator.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open the pairing window: %v", err)
	}
	if err := responder.OpenPairing(model.RolePersonal); err != nil {
		t.Fatalf("open the pairing window: %v", err)
	}

	candidate := waitEvent(t, eventsInitiator, EventPairingCandidate, model.DeviceID{})
	if err := initiator.StartPairing(candidate.Addr); err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	requestInitiator := waitEvent(t, eventsInitiator, EventPairingRequest, model.DeviceID{})
	requestResponder := waitEvent(t, eventsResponder, EventPairingRequest, model.DeviceID{})
	if requestInitiator.SAS.String() != requestResponder.SAS.String() {
		t.Fatalf("the SAS differs: %s and %s", requestInitiator.SAS, requestResponder.SAS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := initiator.ConfirmPairing(ctx, requestInitiator.Peer); err != nil {
		t.Fatalf("confirm on the initiating device: %v", err)
	}
	if err := responder.ConfirmPairing(ctx, requestResponder.Peer); err != nil {
		t.Fatalf("confirm on the accepting device: %v", err)
	}
	waitEvent(t, eventsInitiator, EventPairingPaired, model.DeviceID{})
	waitEvent(t, eventsResponder, EventPairingPaired, model.DeviceID{})
	waitOnline(t, initiator, requestInitiator.Peer)
	waitOnline(t, responder, requestResponder.Peer)
}

// waitTrusted waits until the node trusts the device and returns its record.
func waitTrusted(t *testing.T, n *testNode, subject model.DeviceID) trust.Peer {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		peer, err := n.Trust().Get(context.Background(), subject)
		if err == nil {
			return peer
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s does not trust %s within %v", n.DeviceID(), subject, testTimeout)
	return trust.Peer{}
}

// waitUntrusted checks that the node does not trust the device during a short
// window.
func waitUntrusted(t *testing.T, n *testNode, subject model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := n.Trust().Get(context.Background(), subject); !errors.Is(err, trust.ErrPeerNotFound) {
			t.Fatalf("%s trusts %s: %v", n.DeviceID(), subject, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitSession waits until the node has a live session with the peer.
func waitSession(t *testing.T, n *testNode, peer model.DeviceID) *session.Session {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if l := n.lookup(peer); l != nil {
			l.mu.Lock()
			sess := l.sess
			l.mu.Unlock()
			if sess != nil {
				return sess
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s has no session with %s within %v", n.DeviceID(), peer, testTimeout)
	return nil
}

// sendGossipRecords sends one records message over the session of the node. A
// session that the connection manager replaced during the send is retried.
func sendGossipRecords(t *testing.T, from, to *testNode, records *wirev1.GossipRecords) {
	t.Helper()

	msg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_GossipRecords{GossipRecords: records}}
	deadline := time.Now().Add(testTimeout)
	var err error
	for time.Now().Before(deadline) {
		sess := waitSession(t, from, to.DeviceID())
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		err = sess.Send(ctx, msg)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("send gossip records: %v", err)
}

// sendGossipRecordsUntilViolation sends one records message and waits until the
// receiving session counts it as a protocol violation.
func sendGossipRecordsUntilViolation(t *testing.T, from, to *testNode, records *wirev1.GossipRecords) {
	t.Helper()

	msg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_GossipRecords{GossipRecords: records}}
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		receiving := waitSession(t, to, from.DeviceID())
		sending := waitSession(t, from, to.DeviceID())
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		err := sending.Send(ctx, msg)
		cancel()
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}

		answered := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(answered) {
			if receiving.Violations() > 0 {
				return
			}
			if sessionEnded(receiving) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatalf("%s never counted the message of %s as a violation", to.DeviceID(), from.DeviceID())
}

// sendGossipIntroduction sends an introduction until the peer trusts its
// subject. It keeps the check honest: a message that a replaced session lost is
// sent again.
func sendGossipIntroduction(t *testing.T, from, to *testNode, record *wirev1.Introduction) {
	t.Helper()

	known, err := trust.IntroductionFromProto(record)
	if err != nil {
		t.Fatalf("decode the introduction: %v", err)
	}
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		sendGossipRecords(t, from, to, &wirev1.GossipRecords{
			Introductions: []*wirev1.Introduction{record},
		})
		answered := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(answered) {
			if _, err := to.Trust().Get(context.Background(), known.DeviceID()); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatalf("%s never trusted %s", to.DeviceID(), known.DeviceID())
}

// sessionEnded reports a session that is over.
func sessionEnded(sess *session.Session) bool {
	select {
	case <-sess.Done():
		return true
	default:
		return false
	}
}

// TestGossipTrustsIntroducedDevice checks that the devices of the group learn a
// new Personal device from the device it paired with.
func TestGossipTrustsIntroducedDevice(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 41000)
	b := newTestNode(t, network, bus, 42000)
	n := newTestNode(t, network, bus, 43000)
	for _, node := range []*testNode{a, b, n} {
		node.start(t)
	}

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	pairThroughWindow(t, n, a)

	record := waitTrusted(t, b, n.DeviceID())
	if record.Role != model.RolePersonal {
		t.Fatalf("the introduced device has role %s", record.Role)
	}
	if record.IntroducedBy != a.DeviceID() {
		t.Fatalf("the introduced device came from %s, want %s", record.IntroducedBy, a.DeviceID())
	}

	// The accepting device introduces the rest of the group to the device it
	// paired with.
	known := waitTrusted(t, n, b.DeviceID())
	if known.IntroducedBy != a.DeviceID() {
		t.Fatalf("the known device came from %s, want %s", known.IntroducedBy, a.DeviceID())
	}

	// Both devices of the group find each other once they trust each other.
	waitOnline(t, n, b.DeviceID())
	waitOnline(t, b, n.DeviceID())
}

// TestGossipReachesOfflineDevice checks that a device that was offline while
// another one paired learns the new device when it comes back.
func TestGossipReachesOfflineDevice(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	dir, keystore := t.TempDir(), fake.NewKeystore()
	a := newTestNode(t, network, bus, 44000)
	b := newTestNodeAt(t, network, bus, 45000, dir, keystore)
	n := newTestNode(t, network, bus, 46000)
	for _, node := range []*testNode{a, b, n} {
		node.start(t)
	}

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.Stop(ctx); err != nil {
		t.Fatalf("stop the offline device: %v", err)
	}

	pairThroughWindow(t, n, a)
	if _, err := a.Trust().Get(context.Background(), n.DeviceID()); err != nil {
		t.Fatalf("the accepting device does not trust the paired one: %v", err)
	}

	// The device comes back with the state it had when it went offline.
	restarted := newTestNodeAt(t, network, bus, 47000, dir, keystore)
	restarted.start(t)
	waitOnline(t, restarted, a.DeviceID())

	record := waitTrusted(t, restarted, n.DeviceID())
	if record.IntroducedBy != a.DeviceID() {
		t.Fatalf("the introduced device came from %s, want %s", record.IntroducedBy, a.DeviceID())
	}
}

// TestGossipSpreadsGroupData checks that the discovery key and the relay
// settings reach the other devices of the group.
func TestGossipSpreadsGroupData(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 48000)
	b := newTestNode(t, network, bus, 49000)
	a.start(t)
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	ctx := context.Background()
	relay := trust.RelaySettings{URL: "https://relay.example", Token: "group-token"}
	if err := a.Trust().SetRelaySettings(ctx, relay); err != nil {
		t.Fatalf("set relay settings: %v", err)
	}
	key := bytes.Repeat([]byte{4}, 32)
	at := time.Now().Add(time.Second).Truncate(time.Millisecond)
	if _, err := a.Trust().ApplyGroupData(ctx, trust.GroupData{DiscoveryKey: key, DiscoveryAt: at}); err != nil {
		t.Fatalf("rotate the discovery key: %v", err)
	}

	a.gossip.Notify(a.ctx)

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		data, err := b.Trust().GroupData(ctx)
		if err == nil && bytes.Equal(data.DiscoveryKey, key) && data.Relay == relay {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := b.Trust().GroupData(ctx)
	t.Fatalf("the group data did not reach the peer within %v: %+v, error %v", testTimeout, data, err)
}

// TestGossipRefusesFriendIntroduction checks that a Friends device cannot
// introduce anybody.
func TestGossipRefusesFriendIntroduction(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 50000)
	friend := newTestNode(t, network, bus, 51000)
	a.start(t)
	friend.start(t)
	enableFriends(t, a, friend)

	err := a.AddPeer(context.Background(), trust.Peer{
		Public:   friend.Identity().Public(),
		Name:     friend.cfg.Name,
		Platform: model.PlatformMacOS,
		Role:     model.RoleFriends,
	})
	if err != nil {
		t.Fatalf("add the friend: %v", err)
	}
	err = friend.AddPeer(context.Background(), trust.Peer{
		Public:   a.Identity().Public(),
		Name:     a.cfg.Name,
		Platform: model.PlatformMacOS,
		Role:     model.RoleFriends,
	})
	if err != nil {
		t.Fatalf("add the friend on the other side: %v", err)
	}
	waitOnline(t, a, friend.DeviceID())
	waitOnline(t, friend, a.DeviceID())

	subject := testIdentity(t, 31)
	record, err := trust.NewIntroduction(friend.Identity(), trust.IntroductionInfo{
		Subject:  subject.Public(),
		Name:     "intruder",
		Platform: model.PlatformMacOS,
	}, time.Now())
	if err != nil {
		t.Fatalf("sign the introduction: %v", err)
	}
	sendGossipRecordsUntilViolation(t, friend, a, &wirev1.GossipRecords{
		Introductions: []*wirev1.Introduction{record.Proto()},
	})

	waitUntrusted(t, a, subject.DeviceID())
}

// TestGossipRefusesBrokenIntroduction checks that a forged introduction of a
// Personal device is dropped.
func TestGossipRefusesBrokenIntroduction(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, 52000)
	b := newTestNode(t, network, bus, 53000)
	a.start(t)
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	// A record of the peer itself goes through the same channel.
	known := testIdentity(t, 34)
	record, err := trust.NewIntroduction(b.Identity(), trust.IntroductionInfo{
		Subject:  known.Public(),
		Name:     "known",
		Platform: model.PlatformMacOS,
	}, time.Now())
	if err != nil {
		t.Fatalf("sign the introduction: %v", err)
	}
	sendGossipIntroduction(t, b, a, record.Proto())
	waitTrusted(t, a, known.DeviceID())

	subject := testIdentity(t, 32)
	record, err = trust.NewIntroduction(b.Identity(), trust.IntroductionInfo{
		Subject:  subject.Public(),
		Name:     "forged",
		Platform: model.PlatformMacOS,
	}, time.Now())
	if err != nil {
		t.Fatalf("sign the introduction: %v", err)
	}
	forged := record.Proto()
	forged.Ed25519Sig[0] ^= 0xff
	forged.MldsaSig[0] ^= 0xff
	sendGossipRecords(t, b, a, &wirev1.GossipRecords{
		Introductions: []*wirev1.Introduction{forged},
	})

	waitUntrusted(t, a, subject.DeviceID())

	// A record of a device that is not a peer is refused as well.
	outsider := testIdentity(t, 33)
	record, err = trust.NewIntroduction(outsider, trust.IntroductionInfo{
		Subject:  subject.Public(),
		Platform: model.PlatformMacOS,
	}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("sign the introduction: %v", err)
	}
	sendGossipRecords(t, b, a, &wirev1.GossipRecords{
		Introductions: []*wirev1.Introduction{record.Proto()},
	})
	waitUntrusted(t, a, subject.DeviceID())
}

// testIdentity returns a deterministic identity derived from seed.
func testIdentity(t *testing.T, seed byte) *identity.Identity {
	t.Helper()

	edSeed := bytes.Repeat([]byte{seed}, ed25519.SeedSize)
	pqSeed := bytes.Repeat([]byte{seed, seed + 1}, mldsa.PrivateKeySize/2)
	id, err := identity.FromSeeds(edSeed, pqSeed)
	if err != nil {
		t.Fatalf("create identity %d: %v", seed, err)
	}
	return id
}
