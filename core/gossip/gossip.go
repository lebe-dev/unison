package gossip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Limits of the gossip exchange.
const (
	// MaxDigestHeads bounds the introduction heads of one digest.
	MaxDigestHeads = 1024
	// MaxRecordsPerMessage bounds the introductions of one records message.
	MaxRecordsPerMessage = 16
	// MaxRecordsPerDigest bounds the introductions sent in answer to one
	// digest.
	MaxRecordsPerDigest = 512
	// MaxDigestReplies bounds the digests answered on one session.
	MaxDigestReplies = 8
	// sendTimeout bounds one message to a peer.
	sendTimeout = 5 * time.Second
)

// ErrProtocol reports a gossip message that does not pass validation.
var ErrProtocol = errors.New("gossip: invalid message")

// Store is the part of the trust store the gossip service uses. The trust store
// implements it.
type Store interface {
	// IntroductionHeads returns the subject and the time of every stored
	// introduction.
	IntroductionHeads(ctx context.Context) ([]trust.IntroductionHead, error)
	// IntroductionBySubject returns the stored introduction of the subject.
	IntroductionBySubject(ctx context.Context, id model.DeviceID) (trust.Introduction, error)
	// ApplyIntroduction verifies and stores an introduction and adds the
	// introduced device. It reports whether the device is new.
	ApplyIntroduction(ctx context.Context, intro trust.Introduction) (bool, error)
	// RevocationHeads returns the subject and the time of every stored
	// revocation.
	RevocationHeads(ctx context.Context) ([]trust.RevocationHead, error)
	// RevocationBySubject returns the stored revocation of the subject.
	RevocationBySubject(ctx context.Context, id model.DeviceID) (trust.Revocation, error)
	// ApplyRevocation verifies a revocation and marks the subject revoked. It
	// reports whether this record revoked the subject.
	ApplyRevocation(ctx context.Context, local model.DeviceID, revocation trust.Revocation) (bool, error)
	// GroupData returns the group data of the local device.
	GroupData(ctx context.Context) (trust.GroupData, error)
	// ApplyGroupData stores the newer parts of the group data and reports
	// whether the discovery key changed.
	ApplyGroupData(ctx context.Context, data trust.GroupData) (bool, error)
}

var _ Store = (*trust.Store)(nil)

// Config describes the gossip service.
type Config struct {
	// Store is the trust store. It is required.
	Store Store
	// Local is the device ID of the local device. It is required.
	Local model.DeviceID
	// Logger receives gossip events. Nothing is logged when nil.
	Logger *slog.Logger
	// OnPeersChanged runs after an introduction added a device, so the caller
	// can connect to it.
	OnPeersChanged func(ctx context.Context)
	// OnDiscoveryKeyChanged runs after the discovery key of the group changed.
	OnDiscoveryKeyChanged func(ctx context.Context)
	// OnPeerRevoked runs after a revocation cut a device off, so the caller can
	// close the connection to it.
	OnPeerRevoked func(ctx context.Context, id model.DeviceID)
	// OnSelfRevoked runs after a valid revocation of the local device arrived.
	OnSelfRevoked func(ctx context.Context)
}

// peer is the gossip state of one session.
type peer struct {
	replies int
}

// Service exchanges the trust state of the Personal group on the sessions
// between its devices. Both devices send a digest, then every device sends what
// the other one lacks.
//
// It never talks to Friends devices: the capability matrix refuses their
// introductions.
type Service struct {
	cfg    Config
	log    *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	closed   bool
	sessions map[*session.Session]*peer

	wg sync.WaitGroup
}

// New returns a gossip service. The context bounds the background exchanges.
func New(ctx context.Context, cfg Config) (*Service, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("gossip: nil store")
	case cfg.Local.IsZero():
		return nil, errors.New("gossip: empty local device id")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	serviceCtx, cancel := context.WithCancel(ctx)
	return &Service{
		cfg:      cfg,
		log:      log,
		ctx:      serviceCtx,
		cancel:   cancel,
		sessions: make(map[*session.Session]*peer),
	}, nil
}

// Register adds the gossip handlers to a session. It runs before the handshake,
// so no message of the peer arrives without a handler.
func (s *Service) Register(sess *session.Session) error {
	if sess == nil {
		return errors.New("gossip: nil session")
	}
	if sess.Role() != model.RolePersonal {
		return nil
	}
	if err := sess.Handle(session.KindGossipDigest, s.digestHandler(sess)); err != nil {
		return err
	}
	return sess.Handle(session.KindGossipRecords, s.recordsHandler(sess))
}

// Opened starts the exchange with a peer: the service sends its digest and
// waits for the digest of the peer.
func (s *Service) Opened(sess *session.Session) {
	if sess == nil || sess.Role() != model.RolePersonal {
		return
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if _, ok := s.sessions[sess]; ok {
		s.mu.Unlock()
		return
	}
	s.sessions[sess] = &peer{}
	// The watcher is registered while the service lock is held, so Close cannot
	// start waiting before the goroutine is counted.
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		select {
		case <-sess.Done():
		case <-s.ctx.Done():
		}

		s.mu.Lock()
		delete(s.sessions, sess)
		s.mu.Unlock()
	}()

	s.spawn(func(ctx context.Context) {
		if err := s.sendDigest(ctx, sess, false); err != nil && ctx.Err() == nil {
			s.log.Debug("gossip: digest not sent", "peer", sess.DeviceID(), "error", err)
		}
	})
}

// Notify sends the local digest to every open session, so the group learns about
// data that changed since the session opened.
func (s *Service) Notify(context.Context) {
	for _, sess := range s.sessionList() {
		s.spawn(func(ctx context.Context) {
			if err := s.sendDigest(ctx, sess, false); err != nil && ctx.Err() == nil {
				s.log.Debug("gossip: digest not sent", "peer", sess.DeviceID(), "error", err)
			}
		})
	}
}

// Close stops the background exchanges. It is safe to call more than once.
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.wg.Wait()
		return
	}
	s.closed = true
	s.sessions = make(map[*session.Session]*peer)
	s.mu.Unlock()

	s.cancel()
	s.wg.Wait()
}

// spawn runs fn with the service context on a goroutine that Close waits for.
func (s *Service) spawn(fn func(ctx context.Context)) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		fn(s.ctx)
	}()
}

// sessionList returns a snapshot of the open sessions.
func (s *Service) sessionList() []*session.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := make([]*session.Session, 0, len(s.sessions))
	for sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	return sessions
}

// allowReply takes one digest reply from the budget of the session.
func (s *Service) allowReply(sess *session.Session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.sessions[sess]
	if !ok {
		return false
	}
	if state.replies >= MaxDigestReplies {
		return false
	}
	state.replies++
	return true
}

// sendDigest sends the local trust state to the peer.
func (s *Service) sendDigest(ctx context.Context, sess *session.Session, reply bool) error {
	group, err := s.cfg.Store.GroupData(ctx)
	if err != nil {
		return fmt.Errorf("gossip: read group data: %w", err)
	}
	message := &wirev1.GossipDigest{
		Reply:             reply,
		DiscoveryTsUnixMs: timeToMilli(group.DiscoveryAt),
		RelayTsUnixMs:     timeToMilli(group.RelayAt),
	}

	heads, err := s.cfg.Store.IntroductionHeads(ctx)
	if err != nil {
		return fmt.Errorf("gossip: read introductions: %w", err)
	}
	if len(heads) > MaxDigestHeads {
		s.log.Warn("gossip: digest truncated", "peer", sess.DeviceID(), "heads", len(heads), "limit", MaxDigestHeads)
		heads = heads[:MaxDigestHeads]
	}
	for _, head := range heads {
		message.Introductions = append(message.Introductions, &wirev1.IntroductionHead{
			Subject:  head.Subject.Bytes(),
			TsUnixMs: timeToMilli(head.IssuedAt),
		})
	}

	revocations, err := s.cfg.Store.RevocationHeads(ctx)
	if err != nil {
		return fmt.Errorf("gossip: read revocations: %w", err)
	}
	if len(revocations) > MaxDigestHeads {
		s.log.Warn("gossip: digest truncated", "peer", sess.DeviceID(), "revocations", len(revocations), "limit", MaxDigestHeads)
		revocations = revocations[:MaxDigestHeads]
	}
	for _, head := range revocations {
		message.Revocations = append(message.Revocations, &wirev1.RevocationHead{
			Subject:  head.Subject.Bytes(),
			TsUnixMs: timeToMilli(head.IssuedAt),
		})
	}
	return s.send(ctx, sess, &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_GossipDigest{GossipDigest: message},
	})
}

// digestHandler answers the digest of a peer with what it lacks.
func (s *Service) digestHandler(sess *session.Session) session.Handler {
	return func(ctx context.Context, msg *wirev1.ControlMessage) error {
		digest := msg.GetGossipDigest()
		if digest == nil {
			return fmt.Errorf("%w: empty digest", ErrProtocol)
		}
		heads, err := parseDigest(digest)
		if err != nil {
			return err
		}
		if !digest.GetReply() {
			if !s.allowReply(sess) {
				s.log.Debug("gossip: digest reply limit reached", "peer", sess.DeviceID())
				return nil
			}
			if err := s.sendDigest(ctx, sess, true); err != nil {
				return err
			}
		}
		return s.sendMissing(ctx, sess, digest, heads)
	}
}

// recordsHandler applies the trust data of a peer.
func (s *Service) recordsHandler(sess *session.Session) session.Handler {
	return func(ctx context.Context, msg *wirev1.ControlMessage) error {
		records := msg.GetGossipRecords()
		if records == nil {
			return fmt.Errorf("%w: empty records", ErrProtocol)
		}
		if len(records.GetIntroductions()) > MaxRecordsPerMessage {
			return fmt.Errorf("%w: %d introductions in one message", ErrProtocol, len(records.GetIntroductions()))
		}
		if len(records.GetRevocations()) > MaxRecordsPerMessage {
			return fmt.Errorf("%w: %d revocations in one message", ErrProtocol, len(records.GetRevocations()))
		}

		added := 0
		for _, record := range records.GetIntroductions() {
			intro, err := trust.IntroductionFromProto(record)
			if err != nil {
				s.log.Debug("gossip: introduction refused", "peer", sess.DeviceID(), "error", err)
				continue
			}
			if intro.DeviceID() == s.cfg.Local {
				s.log.Debug("gossip: introduction of the local device ignored", "peer", sess.DeviceID())
				continue
			}
			fresh, err := s.cfg.Store.ApplyIntroduction(ctx, intro)
			if err != nil {
				s.log.Debug("gossip: introduction refused", "peer", sess.DeviceID(), "subject", intro.DeviceID(), "error", err)
				continue
			}
			if !fresh {
				continue
			}
			added++
			s.log.Info("device introduced", "subject", intro.DeviceID(), "issuer", intro.Issuer)
		}
		s.applyRevocations(ctx, sess, records.GetRevocations())

		changed := false
		if group := records.GetGroup(); group != nil {
			data, err := groupFromProto(group)
			if err != nil {
				return err
			}
			changed, err = s.cfg.Store.ApplyGroupData(ctx, data)
			if err != nil {
				return fmt.Errorf("gossip: apply group data: %w", err)
			}
		}

		if changed && s.cfg.OnDiscoveryKeyChanged != nil {
			s.log.Info("discovery key changed")
			s.spawn(s.cfg.OnDiscoveryKeyChanged)
		}
		if added > 0 && s.cfg.OnPeersChanged != nil {
			s.spawn(s.cfg.OnPeersChanged)
		}
		return nil
	}
}

// applyRevocations verifies and applies the revocations of a records message.
// One refused record does not stop the others.
func (s *Service) applyRevocations(ctx context.Context, sess *session.Session, records []*wirev1.Revocation) {
	for _, record := range records {
		revocation, err := trust.RevocationFromProto(record)
		if err != nil {
			s.log.Debug("gossip: revocation refused", "peer", sess.DeviceID(), "error", err)
			continue
		}
		revoked, err := s.cfg.Store.ApplyRevocation(ctx, s.cfg.Local, revocation)
		switch {
		case errors.Is(err, trust.ErrSelfRevocation):
			s.log.Info("the local device was revoked", "issuer", revocation.Issuer)
			if s.cfg.OnSelfRevoked != nil {
				s.spawn(s.cfg.OnSelfRevoked)
			}
		case err != nil:
			s.log.Debug("gossip: revocation refused", "peer", sess.DeviceID(), "subject", revocation.Subject, "error", err)
		case revoked:
			s.log.Info("device revoked", "subject", revocation.Subject, "issuer", revocation.Issuer)
			if s.cfg.OnPeerRevoked != nil {
				s.spawn(func(ctx context.Context) { s.cfg.OnPeerRevoked(ctx, revocation.Subject) })
			}
		}
	}
}

// sendMissing sends the introductions, the revocations and the group data the
// peer lacks.
func (s *Service) sendMissing(ctx context.Context, sess *session.Session, digest *wirev1.GossipDigest, peer digestState) error {
	group, err := s.cfg.Store.GroupData(ctx)
	if err != nil {
		return fmt.Errorf("gossip: read group data: %w", err)
	}
	heads, err := s.cfg.Store.IntroductionHeads(ctx)
	if err != nil {
		return fmt.Errorf("gossip: read introductions: %w", err)
	}
	revocations, err := s.cfg.Store.RevocationHeads(ctx)
	if err != nil {
		return fmt.Errorf("gossip: read revocations: %w", err)
	}

	messages := make([]*wirev1.GossipRecords, 0, 1)
	for _, id := range missingIntroductions(heads, peer.introductions) {
		intro, err := s.cfg.Store.IntroductionBySubject(ctx, id)
		if errors.Is(err, trust.ErrIntroductionNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("gossip: read introduction: %w", err)
		}
		message := recordsMessage(messages)
		message.Introductions = append(message.Introductions, intro.Proto())
		messages = appendIfNew(messages, message)
	}
	for _, id := range missingRevocations(revocations, peer.revocations) {
		revocation, err := s.cfg.Store.RevocationBySubject(ctx, id)
		if errors.Is(err, trust.ErrRevocationNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("gossip: read revocation: %w", err)
		}
		message := recordsMessage(messages)
		message.Revocations = append(message.Revocations, revocation.Proto())
		messages = appendIfNew(messages, message)
	}

	groupRecords := missingGroupData(group, digest)
	if groupRecords == nil && len(messages) == 0 {
		return nil
	}
	if len(messages) == 0 {
		messages = append(messages, &wirev1.GossipRecords{})
	}
	messages[len(messages)-1].Group = groupRecords
	for _, message := range messages {
		if err := s.send(ctx, sess, &wirev1.ControlMessage{
			Body: &wirev1.ControlMessage_GossipRecords{GossipRecords: message},
		}); err != nil {
			return err
		}
	}
	return nil
}

// recordsMessage returns the message to fill next: the last one while it has
// room, a new one otherwise.
func recordsMessage(messages []*wirev1.GossipRecords) *wirev1.GossipRecords {
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		if len(last.Introductions)+len(last.Revocations) < MaxRecordsPerMessage {
			return last
		}
	}
	return &wirev1.GossipRecords{}
}

// appendIfNew adds a message that recordsMessage created.
func appendIfNew(messages []*wirev1.GossipRecords, message *wirev1.GossipRecords) []*wirev1.GossipRecords {
	if len(messages) > 0 && messages[len(messages)-1] == message {
		return messages
	}
	return append(messages, message)
}

// send writes one control message to the peer.
func (s *Service) send(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return sess.Send(ctx, msg)
}

// missingIntroductions returns the subjects whose record is missing on the peer
// or older than the local one.
func missingIntroductions(heads []trust.IntroductionHead, peerHeads map[model.DeviceID]int64) []model.DeviceID {
	missing := make([]model.DeviceID, 0, len(heads))
	for _, head := range heads {
		if ts, ok := peerHeads[head.Subject]; ok && ts >= timeToMilli(head.IssuedAt) {
			continue
		}
		missing = append(missing, head.Subject)
		if len(missing) == MaxRecordsPerDigest {
			break
		}
	}
	return slices.Clip(missing)
}

// missingRevocations returns the subjects whose revocation is missing on the
// peer or older than the local one.
func missingRevocations(heads []trust.RevocationHead, peerHeads map[model.DeviceID]int64) []model.DeviceID {
	missing := make([]model.DeviceID, 0, len(heads))
	for _, head := range heads {
		if ts, ok := peerHeads[head.Subject]; ok && ts >= timeToMilli(head.IssuedAt) {
			continue
		}
		missing = append(missing, head.Subject)
		if len(missing) == MaxRecordsPerDigest {
			break
		}
	}
	return slices.Clip(missing)
}

// missingGroupData returns the group data the peer lacks, or nil when the peer
// is up to date.
func missingGroupData(local trust.GroupData, digest *wirev1.GossipDigest) *wirev1.GroupData {
	discovery := len(local.DiscoveryKey) > 0 && timeToMilli(local.DiscoveryAt) > digest.GetDiscoveryTsUnixMs()
	relay := !local.Relay.Empty() && timeToMilli(local.RelayAt) > digest.GetRelayTsUnixMs()
	if !discovery && !relay {
		return nil
	}

	message := &wirev1.GroupData{}
	if discovery {
		message.DiscoveryKey = local.DiscoveryKey
		message.DiscoveryTsUnixMs = timeToMilli(local.DiscoveryAt)
	}
	if relay {
		message.RelayUrl = local.Relay.URL
		message.RelayToken = []byte(local.Relay.Token)
		message.RelayTsUnixMs = timeToMilli(local.RelayAt)
	}
	return message
}

// digestState is the trust data a digest of a peer reports.
type digestState struct {
	introductions map[model.DeviceID]int64
	revocations   map[model.DeviceID]int64
}

// parseDigest validates a digest and returns its heads.
func parseDigest(digest *wirev1.GossipDigest) (digestState, error) {
	if len(digest.GetIntroductions()) > MaxDigestHeads {
		return digestState{}, fmt.Errorf("%w: digest of %d heads", ErrProtocol, len(digest.GetIntroductions()))
	}
	if len(digest.GetRevocations()) > MaxDigestHeads {
		return digestState{}, fmt.Errorf("%w: digest of %d revocations", ErrProtocol, len(digest.GetRevocations()))
	}

	state := digestState{
		introductions: make(map[model.DeviceID]int64, len(digest.GetIntroductions())),
		revocations:   make(map[model.DeviceID]int64, len(digest.GetRevocations())),
	}
	for _, head := range digest.GetIntroductions() {
		id, err := model.DeviceIDFromBytes(head.GetSubject())
		if err != nil {
			return digestState{}, fmt.Errorf("%w: %w", ErrProtocol, err)
		}
		if id.IsZero() {
			return digestState{}, fmt.Errorf("%w: introduction of an empty device", ErrProtocol)
		}
		if head.GetTsUnixMs() <= 0 {
			return digestState{}, fmt.Errorf("%w: introduction of %s without a time", ErrProtocol, id)
		}
		if stored, ok := state.introductions[id]; !ok || head.GetTsUnixMs() > stored {
			state.introductions[id] = head.GetTsUnixMs()
		}
	}
	for _, head := range digest.GetRevocations() {
		id, err := model.DeviceIDFromBytes(head.GetSubject())
		if err != nil {
			return digestState{}, fmt.Errorf("%w: %w", ErrProtocol, err)
		}
		if id.IsZero() {
			return digestState{}, fmt.Errorf("%w: revocation of an empty device", ErrProtocol)
		}
		if head.GetTsUnixMs() <= 0 {
			return digestState{}, fmt.Errorf("%w: revocation of %s without a time", ErrProtocol, id)
		}
		if stored, ok := state.revocations[id]; !ok || head.GetTsUnixMs() > stored {
			state.revocations[id] = head.GetTsUnixMs()
		}
	}
	return state, nil
}

// groupFromProto validates the group data of a peer.
func groupFromProto(message *wirev1.GroupData) (trust.GroupData, error) {
	key := message.GetDiscoveryKey()
	if len(key) != 0 && len(key) != 32 {
		return trust.GroupData{}, fmt.Errorf("%w: discovery key of %d bytes", ErrProtocol, len(key))
	}
	data := trust.GroupData{
		DiscoveryKey: key,
		DiscoveryAt:  timeFromMilli(message.GetDiscoveryTsUnixMs()),
		Relay: trust.RelaySettings{
			URL:   message.GetRelayUrl(),
			Token: string(message.GetRelayToken()),
		},
		RelayAt: timeFromMilli(message.GetRelayTsUnixMs()),
	}
	if len(data.Relay.URL) > trust.MaxRelayURLLength {
		return trust.GroupData{}, fmt.Errorf("%w: relay address of %d bytes", ErrProtocol, len(data.Relay.URL))
	}
	if len(data.Relay.Token) > trust.MaxRelayTokenLength {
		return trust.GroupData{}, fmt.Errorf("%w: relay token of %d bytes", ErrProtocol, len(data.Relay.Token))
	}
	return data, nil
}

// timeToMilli encodes a time in milliseconds, keeping the zero time as 0.
func timeToMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// timeFromMilli decodes a time encoded by timeToMilli.
func timeFromMilli(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
