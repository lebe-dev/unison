package mediate

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// exchange is one mediated pairing exchange. The service mutex guards it.
type exchange struct {
	id        [sessionIDSize]byte
	idHex     string
	direction Direction
	role      model.Role
	state     State
	expiresAt time.Time
	reason    CancelReason
	sas       pairing.SAS

	// mediator side
	lowID    model.DeviceID
	highID   model.DeviceID
	requestA model.DeviceID
	requestB model.DeviceID
	lowName  string
	highName string
	lowPlat  model.Platform
	highPlat model.Platform
	lowPub   identity.PublicIdentity
	highPub  identity.PublicIdentity
	commit   *wirev1.MediatedCommit
	finish   *wirev1.MediatedFinish

	// incoming side
	mediatorID     model.DeviceID
	mediatorName   string
	mediatorPlat   model.Platform
	peer           identity.PublicIdentity
	peerName       string
	peerPlatform   model.Platform
	peerStored     bool
	localAccepted  bool
	friendsPending bool
	keys           *ephemeralKeys
	timer          *time.Timer
	done           chan struct{}
}

// Service runs mediated pairing exchanges and handles the incoming messages of
// the peers.
type Service struct {
	opts  Options
	clock ports.Clock
	log   *slog.Logger
	ttl   time.Duration

	mu        sync.Mutex
	stopped   bool
	exchanges map[string]*exchange
	replay    *replayCache
	rates     map[model.DeviceID]*rateBucket
}

// rateBucket is the token bucket of one mediator.
type rateBucket struct {
	tokens float64
	last   time.Time
}

// New creates the service.
func New(opts Options) (*Service, error) {
	switch {
	case opts.Identity == nil:
		return nil, errors.New("mediate: identity is required")
	case opts.Trust == nil:
		return nil, errors.New("mediate: trust store is required")
	case opts.Sessions == nil:
		return nil, errors.New("mediate: sessions are required")
	case opts.Platform == model.PlatformUnknown:
		return nil, errors.New("mediate: platform is required")
	}
	clock := opts.Clock
	if clock == nil {
		clock = ports.SystemClock
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Service{
		opts:      opts,
		clock:     clock,
		log:       log,
		ttl:       ttl,
		exchanges: make(map[string]*exchange),
		replay:    &replayCache{},
		rates:     make(map[model.DeviceID]*rateBucket),
	}, nil
}

// Register installs the handlers of the protocol on a new session. The
// mediator and the endpoints share the handlers; the direction of an exchange
// selects the branch.
func (s *Service) Register(sess *session.Session) error {
	handlers := []struct {
		kind    session.Kind
		handler func(context.Context, *session.Session, *wirev1.ControlMessage) error
	}{
		{session.KindMediatedOffer, s.handleOffer},
		{session.KindMediatedReady, s.handleReady},
		{session.KindMediatedCommit, s.handleCommit},
		{session.KindMediatedFinish, s.handleFinish},
		{session.KindMediatedCancel, s.handleCancel},
		{session.KindMediatedResult, s.handleResult},
	}
	for _, entry := range handlers {
		handle := entry.handler
		err := sess.Handle(entry.kind, func(ctx context.Context, msg *wirev1.ControlMessage) error {
			return handle(ctx, sess, msg)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Stop ends every active exchange. The ephemeral keys are wiped.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for id, ex := range s.exchanges {
		s.releaseLocked(ex)
		delete(s.exchanges, id)
	}
}

// Exchanges returns the snapshots of the active exchanges, oldest deadline
// first.
func (s *Service) Exchanges() []Exchange {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Exchange, 0, len(s.exchanges))
	for _, ex := range s.exchanges {
		out = append(out, s.snapshotLocked(ex))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].ExpiresAt.Before(out[j].ExpiresAt)
	})
	return out
}

// Start opens an exchange between peerA and peerB with the given role. The
// caller is the mediator. Both endpoints have to be online and to support the
// capability.
func (s *Service) Start(ctx context.Context, peerA, peerB model.DeviceID, role model.Role) (Exchange, error) {
	if peerA == peerB || peerA.IsZero() || peerB.IsZero() {
		return Exchange{}, fmt.Errorf("%w: two different devices are required", ErrInvalidOffer)
	}
	if role != model.RolePersonal && role != model.RoleFriends {
		return Exchange{}, fmt.Errorf("%w: role %d cannot be proposed", ErrInvalidOffer, role)
	}
	local := s.opts.Identity.Public()
	if peerA == local.DeviceID || peerB == local.DeviceID {
		return Exchange{}, fmt.Errorf("%w: the mediator cannot be an endpoint", ErrInvalidOffer)
	}

	sessionA := s.opts.Sessions.Session(peerA)
	sessionB := s.opts.Sessions.Session(peerB)
	if sessionA == nil || sessionB == nil {
		return Exchange{}, ErrOffline
	}
	if !sessionA.HasCapability(Capability) || !sessionB.HasCapability(Capability) {
		return Exchange{}, ErrUnsupportedPeer
	}

	peerAInfo, err := s.opts.Trust.Get(ctx, peerA)
	if err != nil {
		return Exchange{}, fmt.Errorf("mediate: read peer %s: %w", peerA, err)
	}
	peerBInfo, err := s.opts.Trust.Get(ctx, peerB)
	if err != nil {
		return Exchange{}, fmt.Errorf("mediate: read peer %s: %w", peerB, err)
	}
	if peerAInfo.Revoked || peerBInfo.Revoked {
		return Exchange{}, ErrPeerRevoked
	}
	if !roleWithin(role, peerAInfo.Role) || !roleWithin(role, peerBInfo.Role) {
		return Exchange{}, fmt.Errorf("%w: %s for %s and %s", ErrRoleEscalation, role, peerAInfo.Role, peerBInfo.Role)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return Exchange{}, ErrOffline
	}
	if err := s.checkCapacityLocked(); err != nil {
		return Exchange{}, err
	}
	for _, ex := range s.exchanges {
		if ex.direction != DirectionMediator {
			continue
		}
		if samePair(ex.requestA, ex.requestB, peerA, peerB) {
			return Exchange{}, fmt.Errorf("%w: an exchange with these devices is active", ErrBusy)
		}
	}

	var id [sessionIDSize]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Exchange{}, fmt.Errorf("mediate: generate session id: %w", err)
	}
	idHex := sessionIDString(id[:])
	if s.replay.seen(id) {
		return Exchange{}, fmt.Errorf("%w: duplicate session id", ErrInvalidOffer)
	}

	low, high := orderDevices(peerA, peerB)
	now := s.clock.Now()
	expiresAt := now.Add(s.ttl)
	base := offerTranscript(id, local.DeviceID, low, high, role, expiresAt.UnixMilli())
	signature, err := s.opts.Identity.Sign(OfferSignatureType, base)
	if err != nil {
		return Exchange{}, fmt.Errorf("mediate: sign offer: %w", err)
	}

	lowInfo, highInfo := peerAInfo, peerBInfo
	sessionLow, sessionHigh := sessionA, sessionB
	if low == peerB {
		lowInfo, highInfo = peerBInfo, peerAInfo
		sessionLow, sessionHigh = sessionB, sessionA
	}
	lowSAS, highSAS := lowInfo.Public, highInfo.Public
	ex := &exchange{
		id:         id,
		idHex:      idHex,
		direction:  DirectionMediator,
		role:       role,
		state:      StateWaiting,
		expiresAt:  expiresAt,
		sas:        deriveSAS(id, local.DeviceID, lowSAS, highSAS, role),
		mediatorID: local.DeviceID,
		lowID:      low,
		highID:     high,
		requestA:   peerA,
		requestB:   peerB,
		lowName:    lowInfo.Name,
		highName:   highInfo.Name,
		lowPlat:    lowInfo.Platform,
		highPlat:   highInfo.Platform,
		lowPub:     lowInfo.Public,
		highPub:    highInfo.Public,
		done:       make(chan struct{}),
	}
	s.exchanges[idHex] = ex
	s.replay.add(id)
	s.watchLocked(ex, sessionLow)
	s.watchLocked(ex, sessionHigh)
	s.armTimerLocked(ex)

	offerToLow := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedOffer{MediatedOffer: &wirev1.MediatedOffer{
		SessionId:       append([]byte(nil), id[:]...),
		PeerEd25519:     highInfo.Public.Ed25519,
		PeerMldsa:       highInfo.Public.MLDSABytes(),
		PeerName:        highInfo.Name,
		PeerPlatform:    protoconv.PlatformToProto(highInfo.Platform),
		Role:            protoconv.RoleToProto(role),
		ExpiresAtUnixMs: expiresAt.UnixMilli(),
		Ed25519Sig:      signature.Ed25519,
		MldsaSig:        signature.MLDSA,
	}}}
	offerToHigh := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedOffer{MediatedOffer: &wirev1.MediatedOffer{
		SessionId:       append([]byte(nil), id[:]...),
		PeerEd25519:     lowInfo.Public.Ed25519,
		PeerMldsa:       lowInfo.Public.MLDSABytes(),
		PeerName:        lowInfo.Name,
		PeerPlatform:    protoconv.PlatformToProto(lowInfo.Platform),
		Role:            protoconv.RoleToProto(role),
		ExpiresAtUnixMs: expiresAt.UnixMilli(),
		Ed25519Sig:      signature.Ed25519,
		MldsaSig:        signature.MLDSA,
	}}}

	if err := sessionLow.Send(ctx, offerToLow); err != nil {
		delete(s.exchanges, idHex)
		s.releaseLocked(ex)
		return Exchange{}, fmt.Errorf("mediate: send offer: %w", err)
	}
	if err := sessionHigh.Send(ctx, offerToHigh); err != nil {
		delete(s.exchanges, idHex)
		s.releaseLocked(ex)
		_ = sessionLow.Send(ctx, cancelMessage(id, CancelReasonPeerOffline))
		return Exchange{}, fmt.Errorf("mediate: send offer: %w", err)
	}
	s.log.Info("mediated pairing started", "session", idHex, "low", low, "high", high, "role", role.String())
	s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
	return s.snapshotLocked(ex), nil
}

// Confirm accepts an incoming offer. The higher endpoint answers at once, the
// lower one sends its keys. enableFriendsDiscovery records the choice of the
// user for the completion callback.
func (s *Service) Confirm(ctx context.Context, sessionID string, enableFriendsDiscovery bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionID]
	if ex == nil {
		return ErrNotFound
	}
	if ex.direction != DirectionIncoming {
		return fmt.Errorf("%w: the mediator does not confirm", ErrState)
	}
	if ex.state != StateWaiting && ex.state != StateReady {
		return ErrState
	}
	if s.clock.Now().After(ex.expiresAt) {
		s.endLocked(ctx, ex, CancelReasonExpired, true)
		return ErrExpired
	}
	if ex.localAccepted {
		return nil
	}
	ex.localAccepted = true
	ex.friendsPending = enableFriendsDiscovery

	if s.isLowLocked(ex) {
		keys, err := newEphemeralKeys()
		if err != nil {
			s.endLocked(ctx, ex, CancelReasonFailed, true)
			return err
		}
		ex.keys = keys
		commit, err := s.buildCommitLocked(ex)
		if err != nil {
			s.endLocked(ctx, ex, CancelReasonFailed, true)
			return err
		}
		ex.state = StateAwaitingFinish
		if err := s.sendToLocked(ctx, ex.mediatorID, &wirev1.ControlMessage{
			Body: &wirev1.ControlMessage_MediatedCommit{MediatedCommit: commit},
		}); err != nil {
			s.endLocked(ctx, ex, CancelReasonPeerOffline, false)
			return err
		}
		s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
		return nil
	}

	ex.state = StateReady
	if err := s.sendToLocked(ctx, ex.mediatorID, &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_MediatedReady{MediatedReady: &wirev1.MediatedReady{SessionId: append([]byte(nil), ex.id[:]...)}},
	}); err != nil {
		s.endLocked(ctx, ex, CancelReasonPeerOffline, false)
		return err
	}
	if ex.commit != nil {
		if err := s.finishHighLocked(ctx, ex); err != nil {
			s.endLocked(ctx, ex, CancelReasonPeerOffline, true)
			return err
		}
		return nil
	}
	s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
	return nil
}

// Reject declines an incoming offer.
func (s *Service) Reject(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionID]
	if ex == nil {
		return ErrNotFound
	}
	if ex.direction != DirectionIncoming {
		return fmt.Errorf("%w: the mediator cancels instead", ErrState)
	}
	s.endLocked(ctx, ex, CancelReasonUserRejected, true)
	return nil
}

// Cancel ends an exchange. The mediator tells both endpoints, an endpoint tells
// the mediator.
func (s *Service) Cancel(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionID]
	if ex == nil {
		return ErrNotFound
	}
	if ex.direction == DirectionMediator {
		s.endLocked(ctx, ex, CancelReasonMediatorCanceled, true)
		return nil
	}
	s.endLocked(ctx, ex, CancelReasonUserRejected, true)
	return nil
}

// handleOffer validates an offer and stores it until the user decides.
func (s *Service) handleOffer(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	offer := msg.GetMediatedOffer()
	if offer == nil {
		return ErrInvalidOffer
	}
	now := s.clock.Now()
	mediator := sess.DeviceID()

	peer, err := s.opts.Trust.Get(ctx, mediator)
	if err != nil {
		return fmt.Errorf("mediate: read mediator: %w", err)
	}
	if peer.Revoked {
		return ErrPeerRevoked
	}
	if peer.Decision(trust.ActionMediatedPair) != trust.Confirm {
		return fmt.Errorf("%w: mediator role %s", ErrInvalidOffer, peer.Role)
	}
	role := protoconv.RoleFromProto(offer.GetRole())
	if role != model.RolePersonal && role != model.RoleFriends {
		return fmt.Errorf("%w: role %d", ErrInvalidOffer, role)
	}
	if !roleWithin(role, peer.Role) {
		return violation(fmt.Errorf("%w: role %s for a %s mediator", ErrRoleEscalation, role, peer.Role))
	}
	if !validSessionID(offer.GetSessionId()) {
		return fmt.Errorf("%w: session id of %d bytes", ErrInvalidOffer, len(offer.GetSessionId()))
	}
	var id [sessionIDSize]byte
	copy(id[:], offer.GetSessionId())
	expiresAt := time.UnixMilli(offer.GetExpiresAtUnixMs())
	switch {
	case !now.Before(expiresAt):
		return ErrExpired
	case expiresAt.Sub(now) > s.ttl+MaxClockSkew:
		return fmt.Errorf("%w: deadline is too far ahead", ErrInvalidOffer)
	}

	peerPublic, err := identity.NewPublicIdentity(offer.GetPeerEd25519(), offer.GetPeerMldsa())
	if err != nil {
		return fmt.Errorf("%w: peer identity: %w", ErrInvalidOffer, err)
	}
	local := s.opts.Identity.Public()
	if peerPublic.DeviceID == local.DeviceID {
		return fmt.Errorf("%w: offer of the local device", ErrInvalidOffer)
	}
	low, high := orderDevices(local.DeviceID, peerPublic.DeviceID)
	lowPublic, highPublic := local, peerPublic
	if low == peerPublic.DeviceID {
		lowPublic, highPublic = peerPublic, local
	}
	base := offerTranscript(id, mediator, low, high, role, expiresAt.UnixMilli())
	signature := identity.Signature{Ed25519: offer.GetEd25519Sig(), MLDSA: offer.GetMldsaSig()}
	if err := verifySignature(peer.Public, OfferSignatureType, base, signature); err != nil {
		return violation(fmt.Errorf("%w: offer signature: %w", ErrInvalidOffer, err))
	}

	alreadyPaired := false
	stored, err := s.opts.Trust.Get(ctx, peerPublic.DeviceID)
	switch {
	case err == nil:
		if stored.Revoked {
			return ErrPeerRevoked
		}
		if stored.Role != role {
			return fmt.Errorf("%w: stored %s, offered %s", ErrRoleMismatch, stored.Role, role)
		}
		if s.opts.Sessions.Session(peerPublic.DeviceID) != nil {
			return ErrAlreadyPaired
		}
		alreadyPaired = true
	case errors.Is(err, trust.ErrPeerNotFound):
	default:
		return fmt.Errorf("mediate: read peer: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return ErrOffline
	}
	if s.replay.seen(id) {
		return fmt.Errorf("%w: session id was used already", ErrInvalidOffer)
	}
	if !s.allowLocked(mediator, now) {
		return ErrRateLimited
	}
	pending, fromMediator := 0, 0
	for _, ex := range s.exchanges {
		if ex.direction != DirectionIncoming {
			continue
		}
		pending++
		if ex.mediatorID == mediator {
			fromMediator++
		}
	}
	if pending >= MaxPending || fromMediator >= MaxPendingPerMediator {
		return ErrBusy
	}

	s.replay.add(id)
	ex := &exchange{
		id:           id,
		idHex:        sessionIDString(id[:]),
		direction:    DirectionIncoming,
		role:         role,
		state:        StateWaiting,
		expiresAt:    expiresAt,
		sas:          deriveSAS(id, mediator, lowPublic, highPublic, role),
		mediatorID:   mediator,
		mediatorName: helloName(sess, peer),
		mediatorPlat: peer.Platform,
		lowID:        low,
		highID:       high,
		lowPub:       lowPublic,
		highPub:      highPublic,
		peer:         peerPublic,
		peerName:     sanitizeName(offer.GetPeerName()),
		peerPlatform: protoconv.PlatformFromProto(offer.GetPeerPlatform()),
		peerStored:   alreadyPaired,
		done:         make(chan struct{}),
	}
	s.exchanges[ex.idHex] = ex
	s.armTimerLocked(ex)
	s.watchSessionLocked(ex, sess)
	s.log.Info("mediated pairing offer", "session", ex.idHex, "mediator", mediator, "role", role.String())
	s.emitLocked(Event{Kind: EventOffer, Exchange: s.snapshotLocked(ex)})
	return nil
}

// handleReady records that an endpoint accepted before the keys arrived.
func (s *Service) handleReady(_ context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	ready := msg.GetMediatedReady()
	if ready == nil || !validSessionID(ready.GetSessionId()) {
		return ErrInvalidOffer
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionIDString(ready.GetSessionId())]
	if ex == nil {
		return ErrNotFound
	}
	if ex.direction != DirectionMediator {
		return fmt.Errorf("%w: only the mediator tracks readiness", ErrInvalidOffer)
	}
	if s.clock.Now().After(ex.expiresAt) {
		s.endLocked(context.Background(), ex, CancelReasonExpired, true)
		return ErrExpired
	}
	if sess.DeviceID() != ex.highID {
		return fmt.Errorf("%w: readiness of the wrong endpoint", ErrInvalidOffer)
	}
	ex.state = StateReady
	s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
	return nil
}

// handleCommit verifies the commit of the lower endpoint. The mediator forwards
// it, the higher endpoint stores it until its user accepts.
func (s *Service) handleCommit(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	commit := msg.GetMediatedCommit()
	if commit == nil || !validSessionID(commit.GetSessionId()) {
		return ErrInvalidCommit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionIDString(commit.GetSessionId())]
	if ex == nil {
		return ErrNotFound
	}
	if s.expiredLocked(ctx, ex) {
		return ErrExpired
	}
	if ex.direction == DirectionMediator {
		if sess.DeviceID() != ex.lowID {
			return fmt.Errorf("%w: commit of the wrong endpoint", ErrInvalidCommit)
		}
		if ex.commit != nil {
			return fmt.Errorf("%w: duplicate commit", ErrInvalidCommit)
		}
		if err := s.verifyCommitLocked(ex, commit); err != nil {
			return err
		}
		ex.commit = commit
		ex.state = StateAwaitingFinish
		if err := s.sendToLocked(ctx, ex.highID, msg); err != nil {
			s.endLocked(ctx, ex, CancelReasonPeerOffline, true)
			return err
		}
		s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
		return nil
	}

	if s.isLowLocked(ex) {
		return fmt.Errorf("%w: the lower endpoint does not receive a commit", ErrInvalidCommit)
	}
	if err := s.verifyCommitLocked(ex, commit); err != nil {
		return err
	}
	ex.commit = commit
	ex.peerName = commit.GetName()
	ex.peerPlatform = protoconv.PlatformFromProto(commit.GetPlatform())
	if ex.localAccepted {
		if err := s.finishHighLocked(ctx, ex); err != nil {
			s.endLocked(ctx, ex, CancelReasonPeerOffline, true)
			return err
		}
		return nil
	}
	if ex.state == StateWaiting {
		ex.state = StateReady
	}
	s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
	return nil
}

// handleFinish verifies the finish of the higher endpoint and stores the new
// peer.
func (s *Service) handleFinish(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	finish := msg.GetMediatedFinish()
	if finish == nil || !validSessionID(finish.GetSessionId()) {
		return ErrInvalidFinish
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionIDString(finish.GetSessionId())]
	if ex == nil {
		return ErrNotFound
	}
	if s.expiredLocked(ctx, ex) {
		return ErrExpired
	}
	if ex.direction == DirectionMediator {
		if sess.DeviceID() != ex.highID {
			return fmt.Errorf("%w: finish of the wrong endpoint", ErrInvalidFinish)
		}
		if ex.finish != nil {
			return fmt.Errorf("%w: duplicate finish", ErrInvalidFinish)
		}
		if ex.commit == nil {
			return fmt.Errorf("%w: finish without a commit", ErrInvalidFinish)
		}
		if err := s.verifyFinishLocked(ex, finish); err != nil {
			return err
		}
		ex.finish = finish
		if err := s.sendToLocked(ctx, ex.lowID, msg); err != nil {
			s.endLocked(ctx, ex, CancelReasonPeerOffline, true)
			return err
		}
		s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
		return nil
	}

	if !s.isLowLocked(ex) || ex.state != StateAwaitingFinish || ex.keys == nil {
		return fmt.Errorf("%w: unexpected finish", ErrInvalidFinish)
	}
	if err := s.verifyFinishLocked(ex, finish); err != nil {
		return err
	}
	sharedX, sharedPQ, err := decapsulate(ex.keys, finish.GetX25519Pub(), finish.GetMlkemCt())
	if err != nil {
		s.endLocked(ctx, ex, CancelReasonFailed, true)
		return err
	}
	ex.peerName = finish.GetName()
	ex.peerPlatform = protoconv.PlatformFromProto(finish.GetPlatform())
	base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
	rendezvous := deriveRendezvousKey(base, ex.keys.xPub, ex.keys.ek, finish.GetX25519Pub(), finish.GetMlkemCt(), sharedX, sharedPQ)
	clear(sharedX)
	clear(sharedPQ)

	addrs := normalizeAddrs(finish.GetAddrs(), s.loopbackLocked(ex.mediatorID))
	if err := s.savePeerLocked(ctx, ex, ex.peerName, addrs, rendezvous); err != nil {
		s.endLocked(ctx, ex, reasonOf(err), true)
		return err
	}
	ex.state = StateCompleted
	_ = s.sendToLocked(ctx, ex.mediatorID, &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_MediatedResult{MediatedResult: &wirev1.MediatedResult{
			SessionId: append([]byte(nil), ex.id[:]...),
			Committed: true,
		}},
	})
	s.completeLocked(ctx, ex)
	return nil
}

// handleCancel ends the exchange on this device.
func (s *Service) handleCancel(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	cancel := msg.GetMediatedCancel()
	if cancel == nil || !validSessionID(cancel.GetSessionId()) {
		return ErrInvalidOffer
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionIDString(cancel.GetSessionId())]
	if ex == nil {
		return nil
	}
	reason := cancelReasonFromProto(cancel.GetReason())
	if ex.direction == DirectionIncoming {
		s.endLocked(ctx, ex, reason, false)
		return nil
	}
	if sess.DeviceID() != ex.lowID && sess.DeviceID() != ex.highID {
		return fmt.Errorf("%w: cancel of a foreign device", ErrInvalidOffer)
	}
	other := ex.lowID
	if sess.DeviceID() == ex.lowID {
		other = ex.highID
	}
	_ = s.sendToLocked(ctx, other, msg)
	s.endLocked(ctx, ex, reason, false)
	return nil
}

// handleResult marks an exchange of the mediator as completed.
func (s *Service) handleResult(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	result := msg.GetMediatedResult()
	if result == nil || !validSessionID(result.GetSessionId()) {
		return ErrInvalidFinish
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.exchanges[sessionIDString(result.GetSessionId())]
	if ex == nil {
		return ErrNotFound
	}
	if ex.direction != DirectionMediator || sess.DeviceID() != ex.lowID {
		return fmt.Errorf("%w: result of the wrong endpoint", ErrInvalidFinish)
	}
	if !result.GetCommitted() {
		s.endLocked(ctx, ex, CancelReasonFailed, false)
		return nil
	}
	ex.state = StateCompleted
	s.log.Info("mediated pairing completed", "session", ex.idHex, "low", ex.lowID, "high", ex.highID)
	s.completeLocked(ctx, ex)
	return nil
}

// violation marks an error as a protocol violation of the peer, so the session
// counts it.
func violation(err error) error {
	return fmt.Errorf("%w: %w", session.ErrViolation, err)
}

// expiredLocked ends an exchange past its deadline and reports it.
func (s *Service) expiredLocked(ctx context.Context, ex *exchange) bool {
	if s.clock.Now().Before(ex.expiresAt) {
		return false
	}
	s.endLocked(ctx, ex, CancelReasonExpired, true)
	return true
}

// verifyCommitLocked checks the fields and the signature of a commit.
func (s *Service) verifyCommitLocked(ex *exchange, commit *wirev1.MediatedCommit) error {
	if len(commit.GetX25519Pub()) != x25519Size || len(commit.GetMlkemEk()) != mlkemEncapsulationKeySize {
		return fmt.Errorf("%w: key sizes", ErrInvalidCommit)
	}
	if err := validateName(commit.GetName()); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCommit, err)
	}
	if len(commit.GetAddrs()) > MaxAddrs {
		return fmt.Errorf("%w: %d address hints", ErrInvalidCommit, len(commit.GetAddrs()))
	}
	base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
	transcript := commitTranscript(base, commit.GetX25519Pub(), commit.GetMlkemEk(),
		protoconv.PlatformFromProto(commit.GetPlatform()), commit.GetName(), commit.GetAddrs())
	signature := identity.Signature{Ed25519: commit.GetEd25519Sig(), MLDSA: commit.GetMldsaSig()}
	lowPublic := s.lowPublicLocked(ex)
	if err := verifySignature(lowPublic, CommitSignatureType, transcript, signature); err != nil {
		return violation(fmt.Errorf("%w: commit signature: %w", ErrInvalidCommit, err))
	}
	return nil
}

// verifyFinishLocked checks the fields and the signature of a finish.
func (s *Service) verifyFinishLocked(ex *exchange, finish *wirev1.MediatedFinish) error {
	if len(finish.GetX25519Pub()) != x25519Size || len(finish.GetMlkemCt()) != mlkemCiphertextSize {
		return fmt.Errorf("%w: key sizes", ErrInvalidFinish)
	}
	if err := validateName(finish.GetName()); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidFinish, err)
	}
	if len(finish.GetAddrs()) > MaxAddrs {
		return fmt.Errorf("%w: %d address hints", ErrInvalidFinish, len(finish.GetAddrs()))
	}
	lowX25519, lowEK := ex.lowEphemeral()
	if len(lowX25519) == 0 || len(lowEK) == 0 {
		return fmt.Errorf("%w: missing commit", ErrInvalidFinish)
	}
	base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
	transcript := finishTranscript(base, lowX25519, lowEK, finish.GetX25519Pub(), finish.GetMlkemCt(),
		protoconv.PlatformFromProto(finish.GetPlatform()), finish.GetName(), finish.GetAddrs())
	highPublic := s.highPublicLocked(ex)
	signature := identity.Signature{Ed25519: finish.GetEd25519Sig(), MLDSA: finish.GetMldsaSig()}
	if err := verifySignature(highPublic, FinishSignatureType, transcript, signature); err != nil {
		return violation(fmt.Errorf("%w: finish signature: %w", ErrInvalidFinish, err))
	}
	return nil
}

// lowEphemeral returns the ephemeral keys of the lower endpoint. The mediator
// reads them from the commit it forwarded, the lower endpoint from its own
// exchange state.
func (ex *exchange) lowEphemeral() (x25519Pub, mlkemEK []byte) {
	if ex.commit != nil {
		return ex.commit.GetX25519Pub(), ex.commit.GetMlkemEk()
	}
	if ex.keys != nil {
		return ex.keys.xPub, ex.keys.ek
	}
	return nil, nil
}

// finishHighLocked stores the lower endpoint on the higher one and sends the
// finish message.
func (s *Service) finishHighLocked(ctx context.Context, ex *exchange) error {
	commit := ex.commit
	if commit == nil {
		return fmt.Errorf("%w: missing commit", ErrInvalidCommit)
	}
	share, err := encapsulate(commit.GetX25519Pub(), commit.GetMlkemEk())
	if err != nil {
		return err
	}
	defer share.destroy()

	base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
	rendezvous := deriveRendezvousKey(base, commit.GetX25519Pub(), commit.GetMlkemEk(), share.xPub, share.mlkemCT, share.sharedX, share.sharedPQ)
	addrs := s.localAddrsLocked(ex.mediatorID)
	name := s.opts.Name
	platform := s.opts.Platform
	transcript := finishTranscript(base, commit.GetX25519Pub(), commit.GetMlkemEk(), share.xPub, share.mlkemCT, platform, name, addrs)
	signature, err := s.opts.Identity.Sign(FinishSignatureType, transcript)
	if err != nil {
		return fmt.Errorf("mediate: sign finish: %w", err)
	}
	finish := &wirev1.MediatedFinish{
		SessionId:  append([]byte(nil), ex.id[:]...),
		X25519Pub:  append([]byte(nil), share.xPub...),
		MlkemCt:    append([]byte(nil), share.mlkemCT...),
		Name:       name,
		Platform:   protoconv.PlatformToProto(platform),
		Addrs:      addrs,
		Ed25519Sig: signature.Ed25519,
		MldsaSig:   signature.MLDSA,
	}
	peerAddrs := normalizeAddrs(commit.GetAddrs(), s.loopbackLocked(ex.mediatorID))
	if err := s.savePeerLocked(ctx, ex, commit.GetName(), peerAddrs, rendezvous); err != nil {
		return err
	}
	ex.finish = finish
	ex.state = StateAwaitingFinish
	if err := s.sendToLocked(ctx, ex.mediatorID, &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_MediatedFinish{MediatedFinish: finish},
	}); err != nil {
		return err
	}
	ex.state = StateCompleted
	s.completeLocked(ctx, ex)
	return nil
}

// buildCommitLocked builds and signs the commit of the lower endpoint.
func (s *Service) buildCommitLocked(ex *exchange) (*wirev1.MediatedCommit, error) {
	if ex.keys == nil {
		return nil, fmt.Errorf("%w: missing keys", ErrInvalidCommit)
	}
	addrs := s.localAddrsLocked(ex.mediatorID)
	base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
	transcript := commitTranscript(base, ex.keys.xPub, ex.keys.ek, s.opts.Platform, s.opts.Name, addrs)
	signature, err := s.opts.Identity.Sign(CommitSignatureType, transcript)
	if err != nil {
		return nil, fmt.Errorf("mediate: sign commit: %w", err)
	}
	return &wirev1.MediatedCommit{
		SessionId:  append([]byte(nil), ex.id[:]...),
		X25519Pub:  append([]byte(nil), ex.keys.xPub...),
		MlkemEk:    append([]byte(nil), ex.keys.ek...),
		Name:       s.opts.Name,
		Platform:   protoconv.PlatformToProto(s.opts.Platform),
		Addrs:      addrs,
		Ed25519Sig: signature.Ed25519,
		MldsaSig:   signature.MLDSA,
	}, nil
}

// savePeerLocked writes the new peer and its rendezvous key.
func (s *Service) savePeerLocked(ctx context.Context, ex *exchange, name string, addrs []string, rendezvous []byte) error {
	if err := validateName(name); err != nil {
		return err
	}
	now := s.clock.Now()
	stored, err := s.opts.Trust.Get(ctx, ex.peer.DeviceID)
	switch {
	case err == nil:
		if stored.Revoked {
			return ErrPeerRevoked
		}
		if stored.Role != ex.role {
			return ErrRoleMismatch
		}
	case errors.Is(err, trust.ErrPeerNotFound):
		if err := s.opts.Trust.Add(ctx, trust.Peer{
			Public:       ex.peer,
			Name:         name,
			Platform:     ex.peerPlatform,
			Role:         ex.role,
			IntroducedBy: ex.mediatorID,
			AddedAt:      now,
		}); err != nil {
			return fmt.Errorf("mediate: add peer: %w", err)
		}
	default:
		return fmt.Errorf("mediate: read peer: %w", err)
	}
	if err := s.opts.Trust.SetAnnouncedName(ctx, ex.peer.DeviceID, name); err != nil {
		return fmt.Errorf("mediate: record peer name: %w", err)
	}
	if ex.peerPlatform != model.PlatformUnknown {
		if err := s.opts.Trust.SetPlatform(ctx, ex.peer.DeviceID, ex.peerPlatform); err != nil {
			return fmt.Errorf("mediate: record peer platform: %w", err)
		}
	}
	if len(addrs) > 0 {
		if err := s.opts.Trust.UpdateAddrs(ctx, ex.peer.DeviceID, addrs); err != nil {
			return fmt.Errorf("mediate: record peer addresses: %w", err)
		}
	}
	if err := s.opts.Trust.SetRendezvousKey(ctx, ex.peer.DeviceID, rendezvous); err != nil {
		return fmt.Errorf("mediate: store rendezvous key: %w", err)
	}
	if ex.role == model.RolePersonal {
		intro, err := trust.NewIntroduction(s.opts.Identity, trust.IntroductionInfo{
			Subject:  ex.peer,
			Name:     name,
			Platform: ex.peerPlatform,
		}, now)
		if err != nil {
			return fmt.Errorf("mediate: sign introduction: %w", err)
		}
		if _, err := s.opts.Trust.AddIntroduction(ctx, intro); err != nil {
			return fmt.Errorf("mediate: store introduction: %w", err)
		}
	}
	return nil
}

// completeLocked reports a completed exchange and releases it.
func (s *Service) completeLocked(ctx context.Context, ex *exchange) {
	snapshot := s.snapshotLocked(ex)
	s.releaseLocked(ex)
	delete(s.exchanges, ex.idHex)
	s.emitLocked(Event{Kind: EventCompleted, Exchange: snapshot})
	if s.opts.Completed != nil {
		s.opts.Completed(ctx, snapshot)
	}
}

// endLocked cancels an exchange, tells the peers when possible and reports it.
func (s *Service) endLocked(ctx context.Context, ex *exchange, reason CancelReason, notify bool) {
	if ex.state == StateCompleted {
		return
	}
	ex.state = StateCanceled
	ex.reason = reason
	if notify && ex.direction == DirectionIncoming {
		_ = s.sendToLocked(ctx, ex.mediatorID, cancelMessage(ex.id, reason))
	}
	if notify && ex.direction == DirectionMediator {
		_ = s.sendToLocked(ctx, ex.lowID, cancelMessage(ex.id, reason))
		_ = s.sendToLocked(ctx, ex.highID, cancelMessage(ex.id, reason))
	}
	snapshot := s.snapshotLocked(ex)
	s.releaseLocked(ex)
	delete(s.exchanges, ex.idHex)
	s.log.Info("mediated pairing canceled", "session", ex.idHex, "reason", reason.String())
	s.emitLocked(Event{Kind: EventCanceled, Exchange: snapshot})
}

// releaseLocked stops the timer and wipes the ephemeral keys.
func (s *Service) releaseLocked(ex *exchange) {
	if ex.timer != nil {
		ex.timer.Stop()
		ex.timer = nil
	}
	if ex.keys != nil {
		ex.keys.destroy()
		ex.keys = nil
	}
	if ex.done != nil {
		select {
		case <-ex.done:
		default:
			close(ex.done)
		}
	}
}

// armTimerLocked ends the exchange at its deadline when no message does.
func (s *Service) armTimerLocked(ex *exchange) {
	delay := ex.expiresAt.Sub(s.clock.Now())
	if delay < 0 {
		delay = 0
	}
	id := ex.idHex
	ex.timer = time.AfterFunc(delay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		current := s.exchanges[id]
		if current == nil {
			return
		}
		s.endLocked(context.Background(), current, CancelReasonExpired, true)
	})
}

// watchLocked ends the exchange when a mediator session closes.
func (s *Service) watchLocked(ex *exchange, sess *session.Session) {
	if sess == nil {
		return
	}
	done := sess.Done()
	exchangeID := ex.idHex
	go func() {
		select {
		case <-done:
		case <-ex.done:
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		current := s.exchanges[exchangeID]
		if current == nil {
			return
		}
		s.endLocked(context.Background(), current, CancelReasonPeerOffline, true)
	}()
}

// watchSessionLocked ends an incoming exchange when the mediator session
// closes.
func (s *Service) watchSessionLocked(ex *exchange, sess *session.Session) {
	exchangeID := ex.idHex
	done := sess.Done()
	go func() {
		select {
		case <-done:
		case <-ex.done:
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		current := s.exchanges[exchangeID]
		if current == nil {
			return
		}
		s.endLocked(context.Background(), current, CancelReasonPeerOffline, false)
	}()
}

// sendToLocked writes one message to a peer. A missing session is an offline
// peer.
func (s *Service) sendToLocked(ctx context.Context, id model.DeviceID, msg *wirev1.ControlMessage) error {
	sess := s.opts.Sessions.Session(id)
	if sess == nil {
		return ErrOffline
	}
	if err := sess.Send(ctx, msg); err != nil {
		return fmt.Errorf("mediate: send to %s: %w", id, err)
	}
	return nil
}

// localAddrsLocked returns the validated local address hints.
func (s *Service) localAddrsLocked(mediator model.DeviceID) []string {
	if s.opts.ListenAddrs == nil {
		return nil
	}
	return normalizeAddrs(s.opts.ListenAddrs(), s.loopbackLocked(mediator))
}

// loopbackLocked reports whether hints of the exchange may be loopback.
func (s *Service) loopbackLocked(mediator model.DeviceID) bool {
	return s.opts.LoopbackHint != nil && s.opts.LoopbackHint(mediator)
}

// allowLocked applies the offer rate limit of one mediator.
func (s *Service) allowLocked(mediator model.DeviceID, now time.Time) bool {
	bucket := s.rates[mediator]
	if bucket == nil {
		bucket = &rateBucket{tokens: OfferBurst, last: now}
		s.rates[mediator] = bucket
	}
	elapsed := now.Sub(bucket.last).Seconds()
	if elapsed > 0 {
		bucket.tokens = min(float64(OfferBurst), bucket.tokens+elapsed/OfferRate.Seconds())
		bucket.last = now
	}
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// checkCapacityLocked bounds the exchanges of the mediator.
func (s *Service) checkCapacityLocked() error {
	count := 0
	for _, ex := range s.exchanges {
		if ex.direction == DirectionMediator {
			count++
		}
	}
	if count >= MaxExchanges {
		return ErrBusy
	}
	return nil
}

// snapshotLocked returns the public view of an exchange.
func (s *Service) snapshotLocked(ex *exchange) Exchange {
	snapshot := Exchange{
		SessionID:              ex.idHex,
		Direction:              ex.direction,
		State:                  ex.state,
		Role:                   ex.role,
		SAS:                    ex.sas,
		AlreadyPaired:          ex.peerStored,
		ExpiresAt:              ex.expiresAt,
		CancelReason:           ex.reason,
		EnableFriendsDiscovery: ex.friendsPending,
	}
	if ex.direction == DirectionMediator {
		local := s.opts.Identity.Public()
		snapshot.Mediator = Device{ID: local.DeviceID, Name: s.opts.Name, Platform: s.opts.Platform}
		snapshot.PeerA = Device{ID: ex.requestA, Name: ex.nameOf(ex.requestA), Platform: ex.platformOf(ex.requestA)}
		snapshot.PeerB = Device{ID: ex.requestB, Name: ex.nameOf(ex.requestB), Platform: ex.platformOf(ex.requestB)}
		return snapshot
	}
	snapshot.Mediator = Device{ID: ex.mediatorID, Name: ex.mediatorName, Platform: ex.mediatorPlat}
	snapshot.Peer = Device{ID: ex.peer.DeviceID, Name: ex.peerName, Platform: ex.peerPlatform}
	return snapshot
}

// nameOf returns the display name of an endpoint of a mediator exchange.
func (ex *exchange) nameOf(id model.DeviceID) string {
	if id == ex.lowID {
		return ex.lowName
	}
	return ex.highName
}

// platformOf returns the platform of an endpoint of a mediator exchange.
func (ex *exchange) platformOf(id model.DeviceID) model.Platform {
	if id == ex.lowID {
		return ex.lowPlat
	}
	return ex.highPlat
}

// lowPublicLocked returns the identity of the lower endpoint.
func (s *Service) lowPublicLocked(ex *exchange) identity.PublicIdentity {
	return ex.lowPub
}

// highPublicLocked returns the identity of the higher endpoint.
func (s *Service) highPublicLocked(ex *exchange) identity.PublicIdentity {
	return ex.highPub
}

// isLowLocked reports whether the local device is the lower endpoint.
func (s *Service) isLowLocked(ex *exchange) bool {
	return s.opts.Identity.DeviceID() == ex.lowID
}

// reasonOf maps an error of a completed exchange to a cancel reason.
func reasonOf(err error) CancelReason {
	switch {
	case errors.Is(err, ErrPeerRevoked):
		return CancelReasonRevoked
	case errors.Is(err, ErrRoleMismatch):
		return CancelReasonRoleMismatch
	default:
		return CancelReasonFailed
	}
}

// orderDevices puts two devices into the canonical order.
func orderDevices(one, other model.DeviceID) (low, high model.DeviceID) {
	if bytes.Compare(one[:], other[:]) <= 0 {
		return one, other
	}
	return other, one
}

// samePair reports whether the pair is the same regardless of the order.
func samePair(oneA, oneB, otherA, otherB model.DeviceID) bool {
	return (oneA == otherA && oneB == otherB) || (oneA == otherB && oneB == otherA)
}

// roleWithin reports that role does not exceed the role of the mediator.
func roleWithin(role, mediator model.Role) bool {
	switch mediator {
	case model.RolePersonal:
		return role == model.RolePersonal || role == model.RoleFriends
	case model.RoleFriends:
		return role == model.RoleFriends
	default:
		return false
	}
}

// emitLocked reports an event to the sink.
func (s *Service) emitLocked(event Event) {
	if s.opts.Events != nil {
		s.opts.Events.Report(context.Background(), event)
	}
}

// cancelMessage builds the cancel message of an exchange.
func cancelMessage(id [sessionIDSize]byte, reason CancelReason) *wirev1.ControlMessage {
	return &wirev1.ControlMessage{Body: &wirev1.ControlMessage_MediatedCancel{
		MediatedCancel: &wirev1.MediatedCancel{
			SessionId: append([]byte(nil), id[:]...),
			Reason:    cancelReasonToProto(reason),
		},
	}}
}

// cancelReasonToProto maps a reason to the wire value.
func cancelReasonToProto(reason CancelReason) wirev1.MediatedCancelReason {
	switch reason {
	case CancelReasonUserRejected:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_USER_REJECTED
	case CancelReasonMediatorCanceled:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_MEDIATOR_CANCELED
	case CancelReasonExpired:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_EXPIRED
	case CancelReasonRevoked:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_REVOKED
	case CancelReasonRoleMismatch:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_ROLE_MISMATCH
	case CancelReasonAlreadyPaired:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_ALREADY_PAIRED
	case CancelReasonBusy:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_BUSY
	case CancelReasonPeerOffline:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_PEER_OFFLINE
	case CancelReasonFailed:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_FAILED
	default:
		return wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_UNSPECIFIED
	}
}

// cancelReasonFromProto maps a wire reason to the domain value.
func cancelReasonFromProto(reason wirev1.MediatedCancelReason) CancelReason {
	switch reason {
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_USER_REJECTED:
		return CancelReasonUserRejected
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_MEDIATOR_CANCELED:
		return CancelReasonMediatorCanceled
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_EXPIRED:
		return CancelReasonExpired
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_REVOKED:
		return CancelReasonRevoked
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_ROLE_MISMATCH:
		return CancelReasonRoleMismatch
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_ALREADY_PAIRED:
		return CancelReasonAlreadyPaired
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_BUSY:
		return CancelReasonBusy
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_PEER_OFFLINE:
		return CancelReasonPeerOffline
	case wirev1.MediatedCancelReason_MEDIATED_CANCEL_REASON_FAILED:
		return CancelReasonFailed
	default:
		return CancelReasonFailed
	}
}

// helloName prefers the name the session announced, then the stored one.
func helloName(sess *session.Session, peer trust.Peer) string {
	if name := sess.PeerHello().Name; name != "" {
		return name
	}
	return peer.Name
}

// replayCache remembers recent exchange identifiers.
type replayCache struct {
	entries map[[sessionIDSize]byte]time.Time
}

// seen reports whether the identifier was used within the window.
func (c *replayCache) seen(id [sessionIDSize]byte) bool {
	if c.entries == nil {
		return false
	}
	at, ok := c.entries[id]
	if !ok {
		return false
	}
	if time.Since(at) > ReplayCacheTTL {
		delete(c.entries, id)
		return false
	}
	return true
}

// add records an identifier and prunes the cache.
func (c *replayCache) add(id [sessionIDSize]byte) {
	if c.entries == nil {
		c.entries = make(map[[sessionIDSize]byte]time.Time)
	}
	c.entries[id] = time.Now()
	if len(c.entries) <= ReplayCacheSize {
		return
	}
	var oldestID [sessionIDSize]byte
	var oldest time.Time
	first := true
	for key, at := range c.entries {
		if first || at.Before(oldest) {
			oldestID, oldest, first = key, at, false
		}
	}
	delete(c.entries, oldestID)
}
