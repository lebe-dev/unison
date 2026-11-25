package mediate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// TestReplayCacheRemembersIdentifiers checks that a reused session identifier
// is refused within the window and forgotten after the TTL.
func TestReplayCacheRemembersIdentifiers(t *testing.T) {
	cache := &replayCache{}
	now := time.Unix(1_700_000_000, 0)
	var id [sessionIDSize]byte
	id[0] = 1
	if cache.seen(id, now) {
		t.Fatal("an unused identifier is reported as used")
	}
	cache.add(id, now)
	if !cache.seen(id, now) {
		t.Fatal("a used identifier is forgotten")
	}
	if cache.seen(id, now.Add(ReplayCacheTTL+time.Second)) {
		t.Fatal("an expired identifier is still reported as used")
	}
	id[0] = 2
	if cache.seen(id, now) {
		t.Fatal("another identifier is reported as used")
	}
}

// TestReplayCacheDropsTheOldestEntry checks that the cache stays bounded.
func TestReplayCacheDropsTheOldestEntry(t *testing.T) {
	cache := &replayCache{}
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < ReplayCacheSize+8; i++ {
		var id [sessionIDSize]byte
		id[0] = byte(i)
		id[1] = byte(i >> 8)
		cache.add(id, base.Add(time.Duration(i)*time.Millisecond))
	}
	if len(cache.entries) > ReplayCacheSize {
		t.Fatalf("the cache holds %d entries, limit %d", len(cache.entries), ReplayCacheSize)
	}
	var first [sessionIDSize]byte
	if cache.seen(first, base.Add(time.Second)) {
		t.Fatal("the oldest entry was kept, so the cache did not prune")
	}
}

// TestOfferRateLimit checks the offer rate of one mediator.
func TestOfferRateLimit(t *testing.T) {
	service := &Service{rates: make(map[model.DeviceID]*rateBucket)}
	var mediator model.DeviceID
	mediator[0] = 7
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < OfferBurst; i++ {
		if !service.allowLocked(mediator, now) {
			t.Fatalf("offer %d is refused inside the burst", i)
		}
	}
	if service.allowLocked(mediator, now) {
		t.Fatal("an offer beyond the burst is allowed")
	}
	if !service.allowLocked(mediator, now.Add(OfferRate)) {
		t.Fatal("an offer after the interval is refused")
	}
	other := mediator
	other[1] = 9
	if !service.allowLocked(other, now) {
		t.Fatal("another mediator shares the bucket")
	}
}

// TestRoleWithin checks the monotonic role rule.
func TestRoleWithin(t *testing.T) {
	tests := []struct {
		role, mediator model.Role
		want           bool
	}{
		{model.RolePersonal, model.RolePersonal, true},
		{model.RoleFriends, model.RolePersonal, true},
		{model.RolePersonal, model.RoleFriends, false},
		{model.RoleFriends, model.RoleFriends, true},
		{model.RoleFriends, model.RoleUnspecified, false},
		{model.RoleUnspecified, model.RolePersonal, false},
	}
	for _, test := range tests {
		if got := roleWithin(test.role, test.mediator); got != test.want {
			t.Errorf("%s through %s: %t, want %t", test.role, test.mediator, got, test.want)
		}
	}
}

// TestOrderDevices checks the canonical order of the endpoints.
func TestOrderDevices(t *testing.T) {
	var one, other model.DeviceID
	one[0], other[0] = 1, 2
	low, high := orderDevices(other, one)
	if low != one || high != other {
		t.Fatal("the pair is not ordered by device id")
	}
	if !samePair(one, other, other, one) {
		t.Fatal("the pair depends on the order")
	}
}

// TestValidateAddrHints checks count and per-entry byte limits before a
// transcript is signed or verified.
func TestValidateAddrHints(t *testing.T) {
	if err := validateAddrHints(nil, ErrInvalidCommit); err != nil {
		t.Fatalf("empty list refused: %v", err)
	}
	if err := validateAddrHints([]string{"192.0.2.10:4242", strings.Repeat("a", MaxAddrLength)}, ErrInvalidCommit); err != nil {
		t.Fatalf("valid list refused: %v", err)
	}
	if err := validateAddrHints([]string{""}, ErrInvalidCommit); !errors.Is(err, ErrInvalidCommit) {
		t.Fatalf("empty hint = %v, want ErrInvalidCommit", err)
	}
	if err := validateAddrHints([]string{strings.Repeat("a", MaxAddrLength+1)}, ErrInvalidCommit); !errors.Is(err, ErrInvalidCommit) {
		t.Fatalf("65-byte hint = %v, want ErrInvalidCommit", err)
	}
	// Exceeding uint16 length must never reach appendString.
	if err := validateAddrHints([]string{strings.Repeat("a", 70000)}, ErrInvalidFinish); !errors.Is(err, ErrInvalidFinish) {
		t.Fatalf("70000-byte hint = %v, want ErrInvalidFinish", err)
	}
	tooMany := []string{"192.0.2.1:1", "192.0.2.2:2", "192.0.2.3:3", "192.0.2.4:4", "192.0.2.5:5"}
	if err := validateAddrHints(tooMany, ErrInvalidCommit); !errors.Is(err, ErrInvalidCommit) {
		t.Fatalf("five hints = %v, want ErrInvalidCommit", err)
	}
}

// memStore is an in-memory Store and session.Trust for unit tests.
type memStore struct {
	mu          sync.Mutex
	peers       map[model.DeviceID]trust.Peer
	rendezvous  map[model.DeviceID][]byte
	intros      []trust.Introduction
	getErr      error
	getErrID    model.DeviceID
	storeHasErr bool
}

func newMemStore() *memStore {
	return &memStore{
		peers:      make(map[model.DeviceID]trust.Peer),
		rendezvous: make(map[model.DeviceID][]byte),
	}
}

func (m *memStore) put(peer trust.Peer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[peer.DeviceID()] = peer
}

func (m *memStore) remove(id model.DeviceID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.peers, id)
}

func (m *memStore) revoke(id model.DeviceID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.peers[id]; ok {
		p.Revoked = true
		m.peers[id] = p
	}
}

func (m *memStore) setRole(id model.DeviceID, role model.Role) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.peers[id]; ok {
		p.Role = role
		m.peers[id] = p
	}
}

func (m *memStore) failGet(id model.DeviceID, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getErrID = id
	m.getErr = err
	m.storeHasErr = err != nil
}

func (m *memStore) Get(_ context.Context, id model.DeviceID) (trust.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storeHasErr && id == m.getErrID {
		return trust.Peer{}, m.getErr
	}
	p, ok := m.peers[id]
	if !ok {
		return trust.Peer{}, trust.ErrPeerNotFound
	}
	return p, nil
}

func (m *memStore) Add(_ context.Context, peer trust.Peer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[peer.DeviceID()]; ok {
		return trust.ErrPeerExists
	}
	m.peers[peer.DeviceID()] = peer
	return nil
}

func (m *memStore) Rename(_ context.Context, id model.DeviceID, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	if !ok {
		return trust.ErrPeerNotFound
	}
	p.Name = name
	m.peers[id] = p
	return nil
}

func (m *memStore) SetAnnouncedName(_ context.Context, id model.DeviceID, name string) error {
	return m.Rename(context.Background(), id, name)
}

func (m *memStore) SetPlatform(_ context.Context, id model.DeviceID, platform model.Platform) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	if !ok {
		return trust.ErrPeerNotFound
	}
	p.Platform = platform
	m.peers[id] = p
	return nil
}

func (m *memStore) SetRendezvousKey(_ context.Context, id model.DeviceID, key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rendezvous[id] = append([]byte(nil), key...)
	return nil
}

func (m *memStore) UpdateAddrs(_ context.Context, id model.DeviceID, _ []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[id]; !ok {
		return trust.ErrPeerNotFound
	}
	return nil
}

func (m *memStore) AddIntroduction(_ context.Context, intro trust.Introduction) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.intros = append(m.intros, intro)
	return true, nil
}

// memSessions is a map-backed Sessions provider.
type memSessions struct {
	mu       sync.Mutex
	sessions map[model.DeviceID]*session.Session
}

func newMemSessions() *memSessions {
	return &memSessions{sessions: make(map[model.DeviceID]*session.Session)}
}

func (m *memSessions) set(id model.DeviceID, sess *session.Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess == nil {
		delete(m.sessions, id)
		return
	}
	m.sessions[id] = sess
}

func (m *memSessions) Session(id model.DeviceID) *session.Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// staticVerifier verifies peers against a memStore.
type staticVerifier struct {
	store *memStore
}

func (v staticVerifier) VerifyPeer(_ context.Context, cert *x509.Certificate) (transport.Peer, error) {
	key, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return transport.Peer{}, errors.New("not an ed25519 certificate")
	}
	v.store.mu.Lock()
	defer v.store.mu.Unlock()
	for _, peer := range v.store.peers {
		if bytes.Equal(peer.Public.Ed25519, key) {
			if peer.Revoked {
				return transport.Peer{}, trust.ErrPeerRevoked
			}
			return transport.Peer{Public: peer.Public, Role: peer.Role}, nil
		}
	}
	return transport.Peer{}, trust.ErrPeerNotFound
}

// openSessionPair connects two identities over an in-memory vnet and opens both
// sessions with mediated-pair/1 enabled.
func openSessionPair(
	t *testing.T,
	net *vnet.Network,
	port uint16,
	aID, bID *identity.Identity,
	aStore, bStore *memStore,
	aRole, bRole model.Role,
) *session.Session {
	t.Helper()
	aStore.put(trust.Peer{Public: bID.Public(), Name: "peer-b", Platform: model.PlatformLinux, Role: bRole})
	bStore.put(trust.Peer{Public: aID.Public(), Name: "peer-a", Platform: model.PlatformMacOS, Role: aRole})

	serverPC, err := net.ListenPacket(netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), port).String())
	if err != nil {
		t.Fatalf("listen server: %v", err)
	}
	clientPC, err := net.ListenPacket(netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), port+1).String())
	if err != nil {
		t.Fatalf("listen client: %v", err)
	}

	listener, err := transport.NewListener(serverPC, aID, staticVerifier{store: aStore}, transport.Config{})
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	dialer, err := transport.NewDialer(clientPC, bID, staticVerifier{store: bStore}, transport.Config{})
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acceptCh := make(chan *transport.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			acceptErr <- err
			return
		}
		acceptCh <- conn
	}()

	clientConn, err := dialer.Dial(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var serverConn *transport.Conn
	select {
	case serverConn = <-acceptCh:
	case err := <-acceptErr:
		t.Fatalf("accept: %v", err)
	}

	sessA, err := session.New(serverConn, session.Config{
		Name:         "node-a",
		Platform:     model.PlatformMacOS,
		Capabilities: []string{Capability},
		Trust:        aStore,
	})
	if err != nil {
		t.Fatalf("session A: %v", err)
	}
	sessB, err := session.New(clientConn, session.Config{
		Name:         "node-b",
		Platform:     model.PlatformLinux,
		Capabilities: []string{Capability},
		Trust:        bStore,
	})
	if err != nil {
		t.Fatalf("session B: %v", err)
	}
	t.Cleanup(func() {
		sessA.Close()
		sessB.Close()
	})

	openErr := make(chan error, 1)
	go func() { openErr <- sessA.Open(ctx) }()
	if err := sessB.Open(ctx); err != nil {
		t.Fatalf("open B: %v", err)
	}
	if err := <-openErr; err != nil {
		t.Fatalf("open A: %v", err)
	}
	return sessA
}

func makeSignedOffer(
	t *testing.T,
	mediator *identity.Identity,
	recipient, peer identity.PublicIdentity,
	sessionID [sessionIDSize]byte,
	expiresAt time.Time,
) *wirev1.ControlMessage {
	t.Helper()
	role := model.RolePersonal
	low, high := orderDevices(recipient.DeviceID, peer.DeviceID)
	base := offerTranscript(sessionID, mediator.DeviceID(), low, high, role, expiresAt.UnixMilli())
	sig, err := mediator.Sign(OfferSignatureType, base)
	if err != nil {
		t.Fatalf("sign offer: %v", err)
	}
	return &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedOffer{MediatedOffer: &wirev1.MediatedOffer{
		SessionId:       append([]byte(nil), sessionID[:]...),
		PeerEd25519:     peer.Ed25519,
		PeerMldsa:       peer.MLDSABytes(),
		PeerName:        "Peer",
		PeerPlatform:    protoconv.PlatformToProto(model.PlatformLinux),
		Role:            protoconv.RoleToProto(role),
		ExpiresAtUnixMs: expiresAt.UnixMilli(),
		Ed25519Sig:      sig.Ed25519,
		MldsaSig:        sig.MLDSA,
	}}}
}

func orderedPair(t *testing.T) (low, high *identity.Identity) {
	t.Helper()
	a := newTestIdentity(t)
	b := newTestIdentity(t)
	aID, bID := a.DeviceID(), b.DeviceID()
	if bytes.Compare(aID[:], bID[:]) <= 0 {
		return a, b
	}
	return b, a
}

// TestCheckMediatorFailsClosed checks that Confirm refuses an exchange when
// the mediator is removed, revoked, demoted or unreadable.
func TestCheckMediatorFailsClosed(t *testing.T) {
	net := vnet.NewNetwork()
	t.Cleanup(func() { _ = net.Close() })

	mediator := newTestIdentity(t)
	low, high := orderedPair(t)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))

	cases := []struct {
		name    string
		mutate  func(store *memStore)
		wantErr error
	}{
		{
			name:    "mediator removed from trust store",
			mutate:  func(s *memStore) { s.remove(mediator.DeviceID()) },
			wantErr: ErrPeerRevoked,
		},
		{
			name:    "mediator lookup returns store error",
			mutate:  func(s *memStore) { s.failGet(mediator.DeviceID(), errors.New("db unavailable")) },
			wantErr: nil,
		},
		{
			name:    "mediator revoked",
			mutate:  func(s *memStore) { s.revoke(mediator.DeviceID()) },
			wantErr: ErrPeerRevoked,
		},
		{
			name:    "mediator demoted to friends for personal offer",
			mutate:  func(s *memStore) { s.setRole(mediator.DeviceID(), model.RoleFriends) },
			wantErr: ErrRoleEscalation,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			medStore := newMemStore()
			lowStore := newMemStore()
			sessLow := openSessionPair(t, net, uint16(41000+i*4), low, mediator, lowStore, medStore, model.RolePersonal, model.RolePersonal)
			sessions := newMemSessions()
			sessions.set(mediator.DeviceID(), sessLow)

			svc, err := New(Options{
				Identity: low,
				Trust:    lowStore,
				Sessions: sessions,
				Name:     "Low",
				Platform: model.PlatformMacOS,
				Clock:    clock,
			})
			if err != nil {
				t.Fatalf("new service: %v", err)
			}
			t.Cleanup(svc.Stop)

			var sid [sessionIDSize]byte
			sid[0] = byte(i + 1)
			offer := makeSignedOffer(t, mediator, low.Public(), high.Public(), sid, clock.Now().Add(DefaultTTL))
			if err := svc.handleOffer(context.Background(), sessLow, offer); err != nil {
				t.Fatalf("handleOffer: %v", err)
			}

			tc.mutate(lowStore)
			err = svc.Confirm(context.Background(), sessionIDString(sid[:]), false)
			if err == nil {
				t.Fatal("Confirm succeeded despite invalid mediator state")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Confirm error = %v, want %v", err, tc.wantErr)
			}
			if len(svc.Exchanges()) != 0 {
				t.Fatalf("exchange remained active after failed Confirm")
			}
		})
	}
}

// TestForeignPeerCannotInjectMessages checks that a third paired peer cannot
// inject Commit, Finish or Cancel messages into an incoming exchange.
func TestForeignPeerCannotInjectMessages(t *testing.T) {
	net := vnet.NewNetwork()
	t.Cleanup(func() { _ = net.Close() })

	mediator := newTestIdentity(t)
	attacker := newTestIdentity(t)
	low, high := orderedPair(t)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))

	medStore := newMemStore()
	attStore := newMemStore()
	highStore := newMemStore()
	sessFromMed := openSessionPair(t, net, 41200, high, mediator, highStore, medStore, model.RolePersonal, model.RolePersonal)
	sessFromAtt := openSessionPair(t, net, 41204, high, attacker, highStore, attStore, model.RoleFriends, model.RoleFriends)

	sessions := newMemSessions()
	sessions.set(mediator.DeviceID(), sessFromMed)
	sessions.set(attacker.DeviceID(), sessFromAtt)

	svc, err := New(Options{
		Identity: high,
		Trust:    highStore,
		Sessions: sessions,
		Name:     "High",
		Platform: model.PlatformMacOS,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(svc.Stop)

	var sid [sessionIDSize]byte
	_, _ = rand.Read(sid[:])
	expires := clock.Now().Add(DefaultTTL)
	offer := makeSignedOffer(t, mediator, high.Public(), low.Public(), sid, expires)
	if err := svc.handleOffer(context.Background(), sessFromMed, offer); err != nil {
		t.Fatalf("handleOffer: %v", err)
	}

	// Attacker tries to cancel the incoming exchange.
	if err := svc.handleCancel(context.Background(), sessFromAtt, cancelMessage(sid, CancelReasonUserRejected)); !errors.Is(err, session.ErrViolation) {
		t.Fatalf("foreign cancel = %v, want ErrViolation", err)
	}
	if len(svc.Exchanges()) != 1 {
		t.Fatal("foreign cancel ended the exchange")
	}

	// Attacker tries to inject a commit (even with valid keys/signature).
	keys, err := newEphemeralKeys()
	if err != nil {
		t.Fatalf("ephemeral keys: %v", err)
	}
	defer keys.destroy()
	base := offerTranscript(sid, mediator.DeviceID(), low.DeviceID(), high.DeviceID(), model.RolePersonal, expires.UnixMilli())
	ct := commitTranscript(base, keys.xPub, keys.ek, model.PlatformLinux, "Low", nil)
	csig, err := low.Sign(CommitSignatureType, ct)
	if err != nil {
		t.Fatalf("sign commit: %v", err)
	}
	commitMsg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedCommit{MediatedCommit: &wirev1.MediatedCommit{
		SessionId:  append([]byte(nil), sid[:]...),
		X25519Pub:  append([]byte(nil), keys.xPub...),
		MlkemEk:    append([]byte(nil), keys.ek...),
		Name:       "Low",
		Platform:   protoconv.PlatformToProto(model.PlatformLinux),
		Ed25519Sig: csig.Ed25519,
		MldsaSig:   csig.MLDSA,
	}}}
	if err := svc.handleCommit(context.Background(), sessFromAtt, commitMsg); !errors.Is(err, session.ErrViolation) {
		t.Fatalf("foreign commit = %v, want ErrViolation", err)
	}

	// Valid commit from mediator succeeds and keeps High in StateWaiting until
	// the local user decides; a duplicate commit is then rejected.
	if err := svc.handleCommit(context.Background(), sessFromMed, commitMsg); err != nil {
		t.Fatalf("mediator commit: %v", err)
	}
	exchanges := svc.Exchanges()
	if len(exchanges) != 1 || exchanges[0].State != StateWaiting {
		t.Fatalf("after commit before local confirm: exchanges = %+v, want StateWaiting", exchanges)
	}
	if err := svc.handleCommit(context.Background(), sessFromMed, commitMsg); !errors.Is(err, ErrInvalidCommit) {
		t.Fatalf("duplicate commit = %v, want ErrInvalidCommit", err)
	}

	// Foreign finish on low endpoint is also a violation.
	lowStore := newMemStore()
	lowMedStore := newMemStore()
	lowAttStore := newMemStore()
	sessLowMed := openSessionPair(t, net, 41208, low, mediator, lowStore, lowMedStore, model.RolePersonal, model.RolePersonal)
	sessLowAtt := openSessionPair(t, net, 41212, low, attacker, lowStore, lowAttStore, model.RoleFriends, model.RoleFriends)
	lowSessions := newMemSessions()
	lowSessions.set(mediator.DeviceID(), sessLowMed)
	lowSessions.set(attacker.DeviceID(), sessLowAtt)
	lowSvc, err := New(Options{
		Identity: low,
		Trust:    lowStore,
		Sessions: lowSessions,
		Name:     "Low",
		Platform: model.PlatformLinux,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("low service: %v", err)
	}
	t.Cleanup(lowSvc.Stop)

	offerLow := makeSignedOffer(t, mediator, low.Public(), high.Public(), sid, expires)
	if err := lowSvc.handleOffer(context.Background(), sessLowMed, offerLow); err != nil {
		t.Fatalf("low handleOffer: %v", err)
	}
	if err := lowSvc.Confirm(context.Background(), sessionIDString(sid[:]), false); err != nil {
		t.Fatalf("low Confirm: %v", err)
	}
	finishMsg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedFinish{MediatedFinish: &wirev1.MediatedFinish{
		SessionId: append([]byte(nil), sid[:]...),
		X25519Pub: make([]byte, x25519Size),
		MlkemCt:   make([]byte, mlkemCiphertextSize),
		Name:      "High",
	}}}
	if err := lowSvc.handleFinish(context.Background(), sessLowAtt, finishMsg); !errors.Is(err, session.ErrViolation) {
		t.Fatalf("foreign finish = %v, want ErrViolation", err)
	}
}

// TestMediatorStateInvariants checks that the mediator refuses premature
// MediatedResult before MediatedFinish and does not regress StateAwaitingFinish
// when a late MediatedReady arrives.
func TestMediatorStateInvariants(t *testing.T) {
	net := vnet.NewNetwork()
	t.Cleanup(func() { _ = net.Close() })

	mediator := newTestIdentity(t)
	low, high := orderedPair(t)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))

	medStore := newMemStore()
	lowStore := newMemStore()
	highStore := newMemStore()
	sessLow := openSessionPair(t, net, 41300, mediator, low, medStore, lowStore, model.RolePersonal, model.RolePersonal)
	sessHigh := openSessionPair(t, net, 41304, mediator, high, medStore, highStore, model.RolePersonal, model.RolePersonal)

	sessions := newMemSessions()
	sessions.set(low.DeviceID(), sessLow)
	sessions.set(high.DeviceID(), sessHigh)

	svc, err := New(Options{
		Identity: mediator,
		Trust:    medStore,
		Sessions: sessions,
		Name:     "Mediator",
		Platform: model.PlatformMacOS,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(svc.Stop)

	ex, err := svc.Start(context.Background(), low.DeviceID(), high.DeviceID(), model.RolePersonal)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	svc.mu.Lock()
	raw := svc.exchanges[ex.SessionID]
	sid := raw.id
	expires := raw.expiresAt
	svc.mu.Unlock()

	// Premature result before finish must fail and leave the exchange active.
	resMsg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedResult{MediatedResult: &wirev1.MediatedResult{
		SessionId: append([]byte(nil), sid[:]...),
		Committed: true,
	}}}
	if err := svc.handleResult(context.Background(), sessLow, resMsg); !errors.Is(err, ErrInvalidFinish) {
		t.Fatalf("premature result = %v, want ErrInvalidFinish", err)
	}
	if len(svc.Exchanges()) != 1 {
		t.Fatal("premature result closed the exchange")
	}

	// Commit advances mediator to StateAwaitingFinish.
	keys, err := newEphemeralKeys()
	if err != nil {
		t.Fatalf("ephemeral keys: %v", err)
	}
	defer keys.destroy()
	base := offerTranscript(sid, mediator.DeviceID(), low.DeviceID(), high.DeviceID(), model.RolePersonal, expires.UnixMilli())
	ct := commitTranscript(base, keys.xPub, keys.ek, model.PlatformLinux, "Low", nil)
	csig, err := low.Sign(CommitSignatureType, ct)
	if err != nil {
		t.Fatalf("sign commit: %v", err)
	}
	commitMsg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedCommit{MediatedCommit: &wirev1.MediatedCommit{
		SessionId:  append([]byte(nil), sid[:]...),
		X25519Pub:  append([]byte(nil), keys.xPub...),
		MlkemEk:    append([]byte(nil), keys.ek...),
		Name:       "Low",
		Platform:   protoconv.PlatformToProto(model.PlatformLinux),
		Ed25519Sig: csig.Ed25519,
		MldsaSig:   csig.MLDSA,
	}}}
	if err := svc.handleCommit(context.Background(), sessLow, commitMsg); err != nil {
		t.Fatalf("handleCommit: %v", err)
	}
	if got := svc.Exchanges()[0].State; got != StateAwaitingFinish {
		t.Fatalf("state after commit = %s, want %s", got, StateAwaitingFinish)
	}

	// A late MediatedReady from high must not regress StateAwaitingFinish to StateReady.
	readyMsg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedReady{MediatedReady: &wirev1.MediatedReady{
		SessionId: append([]byte(nil), sid[:]...),
	}}}
	if err := svc.handleReady(context.Background(), sessHigh, readyMsg); err != nil {
		t.Fatalf("handleReady: %v", err)
	}
	if got := svc.Exchanges()[0].State; got != StateAwaitingFinish {
		t.Fatalf("state after late ready = %s, want %s", got, StateAwaitingFinish)
	}
}

// TestHighFinishDoesNotSavePeerOnSendFailure checks that an offline mediator
// during MediatedFinish leaves nothing written to the trust store.
func TestHighFinishDoesNotSavePeerOnSendFailure(t *testing.T) {
	net := vnet.NewNetwork()
	t.Cleanup(func() { _ = net.Close() })

	mediator := newTestIdentity(t)
	low, high := orderedPair(t)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))

	medStore := newMemStore()
	highStore := newMemStore()
	sessHigh := openSessionPair(t, net, 41400, high, mediator, highStore, medStore, model.RolePersonal, model.RolePersonal)

	sessions := newMemSessions()
	sessions.set(mediator.DeviceID(), sessHigh)

	svc, err := New(Options{
		Identity: high,
		Trust:    highStore,
		Sessions: sessions,
		Name:     "High",
		Platform: model.PlatformMacOS,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(svc.Stop)

	var sid [sessionIDSize]byte
	_, _ = rand.Read(sid[:])
	expires := clock.Now().Add(DefaultTTL)
	offer := makeSignedOffer(t, mediator, high.Public(), low.Public(), sid, expires)
	if err := svc.handleOffer(context.Background(), sessHigh, offer); err != nil {
		t.Fatalf("handleOffer: %v", err)
	}
	if err := svc.Confirm(context.Background(), sessionIDString(sid[:]), false); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Simulate mediator going offline right before MediatedFinish is sent.
	sessions.set(mediator.DeviceID(), nil)

	keys, err := newEphemeralKeys()
	if err != nil {
		t.Fatalf("ephemeral keys: %v", err)
	}
	defer keys.destroy()
	base := offerTranscript(sid, mediator.DeviceID(), low.DeviceID(), high.DeviceID(), model.RolePersonal, expires.UnixMilli())
	ct := commitTranscript(base, keys.xPub, keys.ek, model.PlatformLinux, "Low", nil)
	csig, err := low.Sign(CommitSignatureType, ct)
	if err != nil {
		t.Fatalf("sign commit: %v", err)
	}
	commitMsg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedCommit{MediatedCommit: &wirev1.MediatedCommit{
		SessionId:  append([]byte(nil), sid[:]...),
		X25519Pub:  append([]byte(nil), keys.xPub...),
		MlkemEk:    append([]byte(nil), keys.ek...),
		Name:       "Low",
		Platform:   protoconv.PlatformToProto(model.PlatformLinux),
		Ed25519Sig: csig.Ed25519,
		MldsaSig:   csig.MLDSA,
	}}}
	if err := svc.handleCommit(context.Background(), sessHigh, commitMsg); !errors.Is(err, ErrOffline) {
		t.Fatalf("handleCommit with offline mediator = %v, want ErrOffline", err)
	}
	if _, err := highStore.Get(context.Background(), low.DeviceID()); !errors.Is(err, trust.ErrPeerNotFound) {
		t.Fatalf("high stored peer despite failed MediatedFinish send: %v", err)
	}
}

// TestLowFinishRefusesRevokedMediator checks that revoking the mediator after
// Confirm prevents handleFinish from storing the introduced peer.
func TestLowFinishRefusesRevokedMediator(t *testing.T) {
	net := vnet.NewNetwork()
	t.Cleanup(func() { _ = net.Close() })

	mediator := newTestIdentity(t)
	low, high := orderedPair(t)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))

	medStore := newMemStore()
	lowStore := newMemStore()
	sessLow := openSessionPair(t, net, 41500, low, mediator, lowStore, medStore, model.RolePersonal, model.RolePersonal)

	sessions := newMemSessions()
	sessions.set(mediator.DeviceID(), sessLow)

	svc, err := New(Options{
		Identity: low,
		Trust:    lowStore,
		Sessions: sessions,
		Name:     "Low",
		Platform: model.PlatformLinux,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(svc.Stop)

	var sid [sessionIDSize]byte
	_, _ = rand.Read(sid[:])
	expires := clock.Now().Add(DefaultTTL)
	offer := makeSignedOffer(t, mediator, low.Public(), high.Public(), sid, expires)
	if err := svc.handleOffer(context.Background(), sessLow, offer); err != nil {
		t.Fatalf("handleOffer: %v", err)
	}
	if err := svc.Confirm(context.Background(), sessionIDString(sid[:]), false); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	svc.mu.Lock()
	ex := svc.exchanges[sessionIDString(sid[:])]
	lowXPub := append([]byte(nil), ex.keys.xPub...)
	lowEK := append([]byte(nil), ex.keys.ek...)
	svc.mu.Unlock()

	share, err := encapsulate(lowXPub, lowEK)
	if err != nil {
		t.Fatalf("encapsulate: %v", err)
	}
	defer share.destroy()
	base := offerTranscript(sid, mediator.DeviceID(), low.DeviceID(), high.DeviceID(), model.RolePersonal, expires.UnixMilli())
	ft := finishTranscript(base, lowXPub, lowEK, share.xPub, share.mlkemCT, model.PlatformMacOS, "High", nil)
	fsig, err := high.Sign(FinishSignatureType, ft)
	if err != nil {
		t.Fatalf("sign finish: %v", err)
	}
	finishMsg := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedFinish{MediatedFinish: &wirev1.MediatedFinish{
		SessionId:  append([]byte(nil), sid[:]...),
		X25519Pub:  append([]byte(nil), share.xPub...),
		MlkemCt:    append([]byte(nil), share.mlkemCT...),
		Name:       "High",
		Platform:   protoconv.PlatformToProto(model.PlatformMacOS),
		Ed25519Sig: fsig.Ed25519,
		MldsaSig:   fsig.MLDSA,
	}}}

	// Revoke mediator before finish arrives.
	lowStore.revoke(mediator.DeviceID())

	if err := svc.handleFinish(context.Background(), sessLow, finishMsg); !errors.Is(err, ErrPeerRevoked) {
		t.Fatalf("handleFinish after mediator revocation = %v, want ErrPeerRevoked", err)
	}
	if _, err := lowStore.Get(context.Background(), high.DeviceID()); !errors.Is(err, trust.ErrPeerNotFound) {
		t.Fatalf("low stored peer despite revoked mediator: %v", err)
	}
}

// TestOfferValidationEdgeCases checks rejection of mediator self-offers,
// replay before signature verification, dynamic local names, and clock-driven
// expiration.
func TestOfferValidationEdgeCases(t *testing.T) {
	net := vnet.NewNetwork()
	t.Cleanup(func() { _ = net.Close() })

	mediator := newTestIdentity(t)
	low, high := orderedPair(t)
	clock := fake.NewClock(time.Unix(1_700_000_000, 0))

	medStore := newMemStore()
	lowStore := newMemStore()
	sessLow := openSessionPair(t, net, 41600, low, mediator, lowStore, medStore, model.RolePersonal, model.RolePersonal)
	sessions := newMemSessions()
	sessions.set(mediator.DeviceID(), sessLow)

	currentName := "Initial"
	svc, err := New(Options{
		Identity: low,
		Trust:    lowStore,
		Sessions: sessions,
		Name:     "Fallback",
		NameFunc: func() string { return currentName },
		Platform: model.PlatformLinux,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(svc.Stop)

	// 1. Mediator offering itself as peer is refused with ErrInvalidOffer.
	var sidSelf [sessionIDSize]byte
	sidSelf[0] = 0x11
	selfOffer := makeSignedOffer(t, mediator, low.Public(), mediator.Public(), sidSelf, clock.Now().Add(DefaultTTL))
	if err := svc.handleOffer(context.Background(), sessLow, selfOffer); !errors.Is(err, ErrInvalidOffer) {
		t.Fatalf("mediator self-offer = %v, want ErrInvalidOffer", err)
	}

	// 2. Valid offer enters replay cache; replaying the same session ID with
	// garbage signature is refused as ErrInvalidOffer (not a crypto violation).
	var sid [sessionIDSize]byte
	sid[0] = 0x22
	offer := makeSignedOffer(t, mediator, low.Public(), high.Public(), sid, clock.Now().Add(DefaultTTL))
	if err := svc.handleOffer(context.Background(), sessLow, offer); err != nil {
		t.Fatalf("valid offer: %v", err)
	}

	replayed := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedOffer{MediatedOffer: &wirev1.MediatedOffer{
		SessionId:       append([]byte(nil), sid[:]...),
		PeerEd25519:     high.Public().Ed25519,
		PeerMldsa:       high.Public().MLDSABytes(),
		PeerName:        "Peer",
		PeerPlatform:    protoconv.PlatformToProto(model.PlatformLinux),
		Role:            protoconv.RoleToProto(model.RolePersonal),
		ExpiresAtUnixMs: clock.Now().Add(DefaultTTL).UnixMilli(),
		Ed25519Sig:      []byte("bad"),
		MldsaSig:        []byte("bad"),
	}}}
	err = svc.handleOffer(context.Background(), sessLow, replayed)
	if !errors.Is(err, ErrInvalidOffer) || errors.Is(err, session.ErrViolation) {
		t.Fatalf("replayed offer = %v, want ErrInvalidOffer before signature verification", err)
	}

	// 3. Dynamic NameFunc is reflected in buildCommitLocked.
	currentName = "Renamed Device"
	svc.mu.Lock()
	ex := svc.exchanges[sessionIDString(sid[:])]
	ex.keys, err = newEphemeralKeys()
	if err != nil {
		svc.mu.Unlock()
		t.Fatalf("ephemeral keys: %v", err)
	}
	commit, err := svc.buildCommitLocked(ex)
	svc.mu.Unlock()
	if err != nil {
		t.Fatalf("buildCommitLocked: %v", err)
	}
	if commit.GetName() != "Renamed Device" {
		t.Fatalf("commit name = %q, want %q", commit.GetName(), "Renamed Device")
	}

	// 4. Advancing the fake clock past TTL expires the exchange via armTimerLocked.
	clock.Advance(DefaultTTL + time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(svc.Exchanges()) > 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if len(svc.Exchanges()) != 0 {
		t.Fatalf("exchange did not expire after clock advance")
	}
}
