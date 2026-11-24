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
	"github.com/lebe-dev/unison/internal/observability"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// cancelSendTimeout bounds an outbound message sent after the service mutex is
// released, so a stalled peer cannot hold a goroutine indefinitely.
const cancelSendTimeout = 5 * time.Second

// pendingSend is a control message delivered after the service mutex is
// released.
type pendingSend struct {
	target model.DeviceID
	msg    *wirev1.ControlMessage
}

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
	now := s.clock.Now()
	if s.replay.seen(id, now) {
		return Exchange{}, fmt.Errorf("%w: duplicate session id", ErrInvalidOffer)
	}

	low, high := orderDevices(peerA, peerB)
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
	s.replay.add(id, now)
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
	var (
		sends     []pendingSend
		completed *Exchange
		retErr    error
	)

	s.mu.Lock()
	ex := s.exchanges[sessionID]
	switch {
	case ex == nil:
		retErr = ErrNotFound
	case ex.direction != DirectionIncoming:
		retErr = fmt.Errorf("%w: the mediator does not confirm", ErrState)
	case ex.state != StateWaiting && ex.state != StateReady:
		retErr = ErrState
	case !s.clock.Now().Before(ex.expiresAt):
		sends = s.endLocked(ex, CancelReasonExpired, true)
		retErr = ErrExpired
	case ex.localAccepted:
		retErr = nil
	default:
		if reason, err := s.checkMediatorLocked(ctx, ex); err != nil {
			sends = s.endLocked(ex, reason, true)
			retErr = err
		} else {
			ex.localAccepted = true
			ex.friendsPending = enableFriendsDiscovery

			if s.isLowLocked(ex) {
				keys, err := newEphemeralKeys()
				if err != nil {
					sends = s.endLocked(ex, CancelReasonFailed, true)
					retErr = err
				} else {
					ex.keys = keys
					commit, err := s.buildCommitLocked(ex)
					if err != nil {
						sends = s.endLocked(ex, CancelReasonFailed, true)
						retErr = err
					} else {
						ex.state = StateAwaitingFinish
						if err := s.sendToLocked(ctx, ex.mediatorID, &wirev1.ControlMessage{
							Body: &wirev1.ControlMessage_MediatedCommit{MediatedCommit: commit},
						}); err != nil {
							sends = s.endLocked(ex, CancelReasonPeerOffline, false)
							retErr = err
						} else {
							s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
						}
					}
				}
			} else {
				ex.state = StateReady
				if err := s.sendToLocked(ctx, ex.mediatorID, &wirev1.ControlMessage{
					Body: &wirev1.ControlMessage_MediatedReady{MediatedReady: &wirev1.MediatedReady{SessionId: append([]byte(nil), ex.id[:]...)}},
				}); err != nil {
					sends = s.endLocked(ex, CancelReasonPeerOffline, false)
					retErr = err
				} else if ex.commit != nil {
					snap, cancelSends, err := s.finishHighLocked(ctx, ex)
					if err != nil {
						sends = cancelSends
						retErr = err
					} else {
						completed = &snap
					}
				} else {
					s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
				}
			}
		}
	}
	s.mu.Unlock()

	s.sendPending(ctx, sends)
	if completed != nil {
		s.notifyCompleted(ctx, *completed)
	}
	return retErr
}

// Reject declines an incoming offer.
func (s *Service) Reject(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	ex := s.exchanges[sessionID]
	if ex == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	if ex.direction != DirectionIncoming {
		s.mu.Unlock()
		return fmt.Errorf("%w: the mediator cancels instead", ErrState)
	}
	sends := s.endLocked(ex, CancelReasonUserRejected, true)
	s.mu.Unlock()
	s.sendPending(ctx, sends)
	return nil
}

// Cancel ends an exchange. The mediator tells both endpoints, an endpoint tells
// the mediator.
func (s *Service) Cancel(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	ex := s.exchanges[sessionID]
	if ex == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	reason := CancelReasonUserRejected
	if ex.direction == DirectionMediator {
		reason = CancelReasonMediatorCanceled
	}
	sends := s.endLocked(ex, reason, true)
	s.mu.Unlock()
	s.sendPending(ctx, sends)
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
	if peerPublic.DeviceID == mediator {
		return fmt.Errorf("%w: offer of the mediator", ErrInvalidOffer)
	}

	// Refuse stopped services and replayed session IDs before running the
	// hybrid signature verification.
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrOffline
	}
	if s.replay.seen(id, now) {
		s.mu.Unlock()
		return fmt.Errorf("%w: session id was used already", ErrInvalidOffer)
	}
	s.mu.Unlock()

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
	if s.replay.seen(id, now) {
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

	s.replay.add(id, now)
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
func (s *Service) handleReady(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	ready := msg.GetMediatedReady()
	if ready == nil || !validSessionID(ready.GetSessionId()) {
		return ErrInvalidOffer
	}
	s.mu.Lock()
	ex := s.exchanges[sessionIDString(ready.GetSessionId())]
	if ex == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	if ex.direction != DirectionMediator {
		s.mu.Unlock()
		return fmt.Errorf("%w: only the mediator tracks readiness", ErrInvalidOffer)
	}
	if sends, expired := s.expiredLocked(ex); expired {
		s.mu.Unlock()
		s.sendPending(ctx, sends)
		return ErrExpired
	}
	if sess.DeviceID() != ex.highID {
		s.mu.Unlock()
		return fmt.Errorf("%w: readiness of the wrong endpoint", ErrInvalidOffer)
	}
	if ex.state == StateWaiting {
		ex.state = StateReady
		s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
	}
	s.mu.Unlock()
	return nil
}

// handleCommit verifies the commit of the lower endpoint. The mediator forwards
// it, the higher endpoint stores it until its user accepts.
func (s *Service) handleCommit(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	commit := msg.GetMediatedCommit()
	if commit == nil || !validSessionID(commit.GetSessionId()) {
		return ErrInvalidCommit
	}

	var (
		sends     []pendingSend
		completed *Exchange
		retErr    error
	)

	s.mu.Lock()
	ex := s.exchanges[sessionIDString(commit.GetSessionId())]
	if ex == nil {
		retErr = ErrNotFound
	} else if cancelSends, expired := s.expiredLocked(ex); expired {
		sends = cancelSends
		retErr = ErrExpired
	} else if ex.direction == DirectionMediator {
		switch {
		case sess.DeviceID() != ex.lowID:
			retErr = fmt.Errorf("%w: commit of the wrong endpoint", ErrInvalidCommit)
		case ex.commit != nil:
			retErr = fmt.Errorf("%w: duplicate commit", ErrInvalidCommit)
		default:
			if err := s.verifyCommitLocked(ex, commit); err != nil {
				retErr = err
			} else {
				ex.commit = commit
				ex.state = StateAwaitingFinish
				if err := s.sendToLocked(ctx, ex.highID, msg); err != nil {
					sends = s.endLocked(ex, CancelReasonPeerOffline, true)
					retErr = err
				} else {
					s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
				}
			}
		}
	} else {
		switch {
		case sess.DeviceID() != ex.mediatorID:
			retErr = violation(fmt.Errorf("%w: commit of a foreign device", ErrInvalidCommit))
		case s.isLowLocked(ex):
			retErr = fmt.Errorf("%w: the lower endpoint does not receive a commit", ErrInvalidCommit)
		case ex.commit != nil:
			retErr = fmt.Errorf("%w: duplicate commit", ErrInvalidCommit)
		default:
			if err := s.verifyCommitLocked(ex, commit); err != nil {
				retErr = err
			} else {
				ex.commit = commit
				ex.peerName = commit.GetName()
				ex.peerPlatform = protoconv.PlatformFromProto(commit.GetPlatform())
				if ex.localAccepted {
					snap, cancelSends, err := s.finishHighLocked(ctx, ex)
					if err != nil {
						sends = cancelSends
						retErr = err
					} else {
						completed = &snap
					}
				} else {
					// Keep StateWaiting until the local user decides, so
					// prompts and notifications stay visible.
					s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
				}
			}
		}
	}
	s.mu.Unlock()

	s.sendPending(ctx, sends)
	if completed != nil {
		s.notifyCompleted(ctx, *completed)
	}
	return retErr
}

// handleFinish verifies the finish of the higher endpoint and stores the new
// peer.
func (s *Service) handleFinish(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	finish := msg.GetMediatedFinish()
	if finish == nil || !validSessionID(finish.GetSessionId()) {
		return ErrInvalidFinish
	}

	var (
		sends     []pendingSend
		completed *Exchange
		retErr    error
	)

	s.mu.Lock()
	ex := s.exchanges[sessionIDString(finish.GetSessionId())]
	if ex == nil {
		retErr = ErrNotFound
	} else if cancelSends, expired := s.expiredLocked(ex); expired {
		sends = cancelSends
		retErr = ErrExpired
	} else if ex.direction == DirectionMediator {
		switch {
		case sess.DeviceID() != ex.highID:
			retErr = fmt.Errorf("%w: finish of the wrong endpoint", ErrInvalidFinish)
		case ex.finish != nil:
			retErr = fmt.Errorf("%w: duplicate finish", ErrInvalidFinish)
		case ex.commit == nil:
			retErr = fmt.Errorf("%w: finish without a commit", ErrInvalidFinish)
		default:
			if err := s.verifyFinishLocked(ex, finish); err != nil {
				retErr = err
			} else {
				ex.finish = finish
				if err := s.sendToLocked(ctx, ex.lowID, msg); err != nil {
					sends = s.endLocked(ex, CancelReasonPeerOffline, true)
					retErr = err
				} else {
					s.emitLocked(Event{Kind: EventUpdated, Exchange: s.snapshotLocked(ex)})
				}
			}
		}
	} else {
		switch {
		case sess.DeviceID() != ex.mediatorID:
			retErr = violation(fmt.Errorf("%w: finish of a foreign device", ErrInvalidFinish))
		case !s.isLowLocked(ex) || ex.state != StateAwaitingFinish || ex.keys == nil:
			retErr = fmt.Errorf("%w: unexpected finish", ErrInvalidFinish)
		default:
			if err := s.verifyFinishLocked(ex, finish); err != nil {
				retErr = err
			} else if reason, err := s.checkMediatorLocked(ctx, ex); err != nil {
				sends = s.endLocked(ex, reason, true)
				retErr = err
			} else {
				sharedX, sharedPQ, err := decapsulate(ex.keys, finish.GetX25519Pub(), finish.GetMlkemCt())
				if err != nil {
					sends = s.endLocked(ex, CancelReasonFailed, true)
					retErr = err
				} else {
					ex.peerName = finish.GetName()
					ex.peerPlatform = protoconv.PlatformFromProto(finish.GetPlatform())
					base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
					rendezvous := deriveRendezvousKey(base, ex.keys.xPub, ex.keys.ek, finish.GetX25519Pub(), finish.GetMlkemCt(), sharedX, sharedPQ)
					clear(sharedX)
					clear(sharedPQ)

					addrs := normalizeAddrs(finish.GetAddrs(), s.loopbackLocked(ex.mediatorID))
					saveErr := s.savePeerLocked(ctx, ex, ex.peerName, addrs, rendezvous)
					clear(rendezvous)
					if saveErr != nil {
						sends = s.endLocked(ex, reasonOf(saveErr), true)
						retErr = saveErr
					} else {
						ex.state = StateCompleted
						sends = []pendingSend{{
							target: ex.mediatorID,
							msg: &wirev1.ControlMessage{
								Body: &wirev1.ControlMessage_MediatedResult{MediatedResult: &wirev1.MediatedResult{
									SessionId: append([]byte(nil), ex.id[:]...),
									Committed: true,
								}},
							},
						}}
						snap := s.completeLocked(ex)
						completed = &snap
					}
				}
			}
		}
	}
	s.mu.Unlock()

	s.sendPending(ctx, sends)
	if completed != nil {
		s.notifyCompleted(ctx, *completed)
	}
	return retErr
}

// handleCancel ends the exchange on this device.
func (s *Service) handleCancel(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	cancel := msg.GetMediatedCancel()
	if cancel == nil || !validSessionID(cancel.GetSessionId()) {
		return ErrInvalidOffer
	}
	var (
		sends  []pendingSend
		retErr error
	)
	s.mu.Lock()
	ex := s.exchanges[sessionIDString(cancel.GetSessionId())]
	if ex != nil {
		reason := cancelReasonFromProto(cancel.GetReason())
		switch {
		case ex.direction == DirectionIncoming:
			if sess.DeviceID() != ex.mediatorID {
				retErr = violation(fmt.Errorf("%w: cancel of a foreign device", ErrInvalidOffer))
			} else {
				sends = s.endLocked(ex, reason, false)
			}
		case sess.DeviceID() != ex.lowID && sess.DeviceID() != ex.highID:
			retErr = fmt.Errorf("%w: cancel of a foreign device", ErrInvalidOffer)
		default:
			other := ex.lowID
			if sess.DeviceID() == ex.lowID {
				other = ex.highID
			}
			sends = append(s.endLocked(ex, reason, false), pendingSend{target: other, msg: msg})
		}
	}
	s.mu.Unlock()

	s.sendPending(ctx, sends)
	return retErr
}

// handleResult marks an exchange of the mediator as completed.
func (s *Service) handleResult(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	result := msg.GetMediatedResult()
	if result == nil || !validSessionID(result.GetSessionId()) {
		return ErrInvalidFinish
	}
	var (
		sends     []pendingSend
		completed *Exchange
		retErr    error
	)
	s.mu.Lock()
	ex := s.exchanges[sessionIDString(result.GetSessionId())]
	switch {
	case ex == nil:
		retErr = ErrNotFound
	case ex.direction != DirectionMediator || sess.DeviceID() != ex.lowID:
		retErr = fmt.Errorf("%w: result of the wrong endpoint", ErrInvalidFinish)
	case ex.finish == nil:
		retErr = fmt.Errorf("%w: result before finish", ErrInvalidFinish)
	case !result.GetCommitted():
		sends = s.endLocked(ex, CancelReasonFailed, false)
	default:
		ex.state = StateCompleted
		s.log.Info("mediated pairing completed", "session", ex.idHex, "low", ex.lowID, "high", ex.highID)
		snap := s.completeLocked(ex)
		completed = &snap
	}
	s.mu.Unlock()

	s.sendPending(ctx, sends)
	if completed != nil {
		s.notifyCompleted(ctx, *completed)
	}
	return retErr
}

// violation marks an error as a protocol violation of the peer, so the session
// counts it.
func violation(err error) error {
	return fmt.Errorf("%w: %w", session.ErrViolation, err)
}

// expiredLocked ends an exchange past its deadline and reports it.
func (s *Service) expiredLocked(ex *exchange) ([]pendingSend, bool) {
	if s.clock.Now().Before(ex.expiresAt) {
		return nil, false
	}
	return s.endLocked(ex, CancelReasonExpired, true), true
}

// validateAddrHints checks that the list of address hints obeys the protocol
// count and per-entry byte limits before it enters a signed transcript.
func validateAddrHints(addrs []string, kind error) error {
	if len(addrs) > MaxAddrs {
		return fmt.Errorf("%w: %d address hints", kind, len(addrs))
	}
	for _, addr := range addrs {
		if len(addr) == 0 || len(addr) > MaxAddrLength {
			return fmt.Errorf("%w: address hint of %d bytes", kind, len(addr))
		}
	}
	return nil
}

// verifyCommitLocked checks the fields and the signature of a commit.
func (s *Service) verifyCommitLocked(ex *exchange, commit *wirev1.MediatedCommit) error {
	if len(commit.GetX25519Pub()) != x25519Size || len(commit.GetMlkemEk()) != mlkemEncapsulationKeySize {
		return fmt.Errorf("%w: key sizes", ErrInvalidCommit)
	}
	if err := validateName(commit.GetName()); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCommit, err)
	}
	if err := validateAddrHints(commit.GetAddrs(), ErrInvalidCommit); err != nil {
		return err
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
	if err := validateAddrHints(finish.GetAddrs(), ErrInvalidFinish); err != nil {
		return err
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

// checkMediatorLocked verifies that the mediator is still trusted, unrevoked
// and allowed to introduce the role of the exchange. It fails closed on store
// errors so a removed or unreadable mediator cannot complete an exchange.
func (s *Service) checkMediatorLocked(ctx context.Context, ex *exchange) (CancelReason, error) {
	mediator, err := s.opts.Trust.Get(ctx, ex.mediatorID)
	if err != nil {
		if errors.Is(err, trust.ErrPeerNotFound) {
			return CancelReasonRevoked, ErrPeerRevoked
		}
		return CancelReasonFailed, fmt.Errorf("mediate: read mediator: %w", err)
	}
	if mediator.Revoked {
		return CancelReasonRevoked, ErrPeerRevoked
	}
	if mediator.Decision(trust.ActionMediatedPair) != trust.Confirm {
		return CancelReasonFailed, fmt.Errorf("%w: mediator role %s", ErrInvalidOffer, mediator.Role)
	}
	if !roleWithin(ex.role, mediator.Role) {
		return CancelReasonRoleMismatch, ErrRoleEscalation
	}
	return CancelReasonNone, nil
}

// checkPeerSaveLocked validates the peer name and existing trust state before
// the finish message is sent or stored.
func (s *Service) checkPeerSaveLocked(ctx context.Context, ex *exchange, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	stored, err := s.opts.Trust.Get(ctx, ex.peer.DeviceID)
	switch {
	case err == nil:
		if stored.Revoked {
			return ErrPeerRevoked
		}
		if stored.Role != ex.role {
			return ErrRoleMismatch
		}
		return nil
	case errors.Is(err, trust.ErrPeerNotFound):
		return nil
	default:
		return fmt.Errorf("mediate: read peer: %w", err)
	}
}

// finishHighLocked validates trust, sends the finish message and stores the
// lower endpoint on the higher one. Sending precedes storage so an offline
// mediator never leaves a half-committed peer in the trust store.
func (s *Service) finishHighLocked(ctx context.Context, ex *exchange) (Exchange, []pendingSend, error) {
	commit := ex.commit
	if commit == nil {
		return Exchange{}, s.endLocked(ex, CancelReasonFailed, true), fmt.Errorf("%w: missing commit", ErrInvalidCommit)
	}
	if reason, err := s.checkMediatorLocked(ctx, ex); err != nil {
		return Exchange{}, s.endLocked(ex, reason, true), err
	}
	if err := s.checkPeerSaveLocked(ctx, ex, commit.GetName()); err != nil {
		return Exchange{}, s.endLocked(ex, reasonOf(err), true), err
	}
	share, err := encapsulate(commit.GetX25519Pub(), commit.GetMlkemEk())
	if err != nil {
		return Exchange{}, s.endLocked(ex, CancelReasonFailed, true), err
	}
	defer share.destroy()

	base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
	rendezvous := deriveRendezvousKey(base, commit.GetX25519Pub(), commit.GetMlkemEk(), share.xPub, share.mlkemCT, share.sharedX, share.sharedPQ)
	defer clear(rendezvous)

	addrs := s.localAddrsLocked(ex.mediatorID)
	name := s.localName()
	platform := s.opts.Platform
	transcript := finishTranscript(base, commit.GetX25519Pub(), commit.GetMlkemEk(), share.xPub, share.mlkemCT, platform, name, addrs)
	signature, err := s.opts.Identity.Sign(FinishSignatureType, transcript)
	if err != nil {
		return Exchange{}, s.endLocked(ex, CancelReasonFailed, true), fmt.Errorf("mediate: sign finish: %w", err)
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
	ex.finish = finish
	ex.state = StateAwaitingFinish
	if err := s.sendToLocked(ctx, ex.mediatorID, &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_MediatedFinish{MediatedFinish: finish},
	}); err != nil {
		return Exchange{}, s.endLocked(ex, CancelReasonPeerOffline, false), err
	}
	peerAddrs := normalizeAddrs(commit.GetAddrs(), s.loopbackLocked(ex.mediatorID))
	if err := s.savePeerLocked(ctx, ex, commit.GetName(), peerAddrs, rendezvous); err != nil {
		return Exchange{}, s.endLocked(ex, reasonOf(err), true), err
	}
	ex.state = StateCompleted
	return s.completeLocked(ex), nil, nil
}

// buildCommitLocked builds and signs the commit of the lower endpoint.
func (s *Service) buildCommitLocked(ex *exchange) (*wirev1.MediatedCommit, error) {
	if ex.keys == nil {
		return nil, fmt.Errorf("%w: missing keys", ErrInvalidCommit)
	}
	name := s.localName()
	addrs := s.localAddrsLocked(ex.mediatorID)
	base := offerTranscript(ex.id, ex.mediatorID, ex.lowID, ex.highID, ex.role, ex.expiresAt.UnixMilli())
	transcript := commitTranscript(base, ex.keys.xPub, ex.keys.ek, s.opts.Platform, name, addrs)
	signature, err := s.opts.Identity.Sign(CommitSignatureType, transcript)
	if err != nil {
		return nil, fmt.Errorf("mediate: sign commit: %w", err)
	}
	return &wirev1.MediatedCommit{
		SessionId:  append([]byte(nil), ex.id[:]...),
		X25519Pub:  append([]byte(nil), ex.keys.xPub...),
		MlkemEk:    append([]byte(nil), ex.keys.ek...),
		Name:       name,
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

// completeLocked reports a completed exchange, releases it and returns the
// snapshot for the completion callback.
func (s *Service) completeLocked(ex *exchange) Exchange {
	snapshot := s.snapshotLocked(ex)
	s.releaseLocked(ex)
	delete(s.exchanges, ex.idHex)
	s.emitLocked(Event{Kind: EventCompleted, Exchange: snapshot})
	return snapshot
}

// notifyCompleted runs the completion callback without holding the service
// mutex.
func (s *Service) notifyCompleted(ctx context.Context, snapshot Exchange) {
	if s.opts.Completed != nil {
		s.opts.Completed(ctx, snapshot)
	}
}

// endLocked cancels an exchange, reports it and returns any cancel messages to
// deliver after releasing the service mutex.
func (s *Service) endLocked(ex *exchange, reason CancelReason, notify bool) []pendingSend {
	if ex.state == StateCompleted {
		return nil
	}
	ex.state = StateCanceled
	ex.reason = reason
	var sends []pendingSend
	if notify && ex.direction == DirectionIncoming {
		sends = append(sends, pendingSend{target: ex.mediatorID, msg: cancelMessage(ex.id, reason)})
	}
	if notify && ex.direction == DirectionMediator {
		msg := cancelMessage(ex.id, reason)
		sends = append(sends,
			pendingSend{target: ex.lowID, msg: msg},
			pendingSend{target: ex.highID, msg: msg},
		)
	}
	snapshot := s.snapshotLocked(ex)
	s.releaseLocked(ex)
	delete(s.exchanges, ex.idHex)
	s.log.Info("mediated pairing canceled", "session", ex.idHex, "reason", reason.String())
	s.emitLocked(Event{Kind: EventCanceled, Exchange: snapshot})
	return sends
}

// sendPending delivers queued control messages without holding the service
// mutex. A context without a deadline gets a bounded timeout so a stalled peer
// cannot block indefinitely.
func (s *Service) sendPending(ctx context.Context, sends []pendingSend) {
	if len(sends) == 0 {
		return
	}
	sendCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		sendCtx, cancel = context.WithTimeout(ctx, cancelSendTimeout)
		defer cancel()
	}
	for _, item := range sends {
		_ = s.sendToLocked(sendCtx, item.target, item.msg)
	}
}

// releaseLocked wipes the ephemeral keys and signals the exchange watchers to
// stop.
func (s *Service) releaseLocked(ex *exchange) {
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
	done := ex.done
	timer := s.clock.After(delay)
	go func() {
		defer observability.Recover()
		select {
		case <-done:
			return
		case <-timer:
		}
		s.mu.Lock()
		current := s.exchanges[id]
		if current == nil || s.clock.Now().Before(current.expiresAt) {
			s.mu.Unlock()
			return
		}
		sends := s.endLocked(current, CancelReasonExpired, true)
		s.mu.Unlock()
		s.sendPending(context.Background(), sends)
	}()
}

// watchLocked ends the exchange when a mediator session closes.
func (s *Service) watchLocked(ex *exchange, sess *session.Session) {
	if sess == nil {
		return
	}
	done := sess.Done()
	exchangeID := ex.idHex
	exDone := ex.done
	go func() {
		defer observability.Recover()
		select {
		case <-done:
		case <-exDone:
			return
		}
		s.mu.Lock()
		current := s.exchanges[exchangeID]
		if current == nil {
			s.mu.Unlock()
			return
		}
		sends := s.endLocked(current, CancelReasonPeerOffline, true)
		s.mu.Unlock()
		s.sendPending(context.Background(), sends)
	}()
}

// watchSessionLocked ends an incoming exchange when the mediator session
// closes.
func (s *Service) watchSessionLocked(ex *exchange, sess *session.Session) {
	exchangeID := ex.idHex
	done := sess.Done()
	exDone := ex.done
	go func() {
		defer observability.Recover()
		select {
		case <-done:
		case <-exDone:
			return
		}
		s.mu.Lock()
		current := s.exchanges[exchangeID]
		if current == nil {
			s.mu.Unlock()
			return
		}
		sends := s.endLocked(current, CancelReasonPeerOffline, false)
		s.mu.Unlock()
		s.sendPending(context.Background(), sends)
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

// localName returns the current display name of this device.
func (s *Service) localName() string {
	if s.opts.NameFunc != nil {
		if name := s.opts.NameFunc(); name != "" {
			return name
		}
	}
	return s.opts.Name
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
		snapshot.Mediator = Device{ID: local.DeviceID, Name: s.localName(), Platform: s.opts.Platform}
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
func (c *replayCache) seen(id [sessionIDSize]byte, now time.Time) bool {
	if c.entries == nil {
		return false
	}
	at, ok := c.entries[id]
	if !ok {
		return false
	}
	if now.Sub(at) > ReplayCacheTTL {
		delete(c.entries, id)
		return false
	}
	return true
}

// add records an identifier and prunes the cache.
func (c *replayCache) add(id [sessionIDSize]byte, now time.Time) {
	if c.entries == nil {
		c.entries = make(map[[sessionIDSize]byte]time.Time)
	}
	c.entries[id] = now
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
