package share

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/trust"
	"github.com/lebe-dev/unison/internal/observability"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Trust reads the peers the capability matrix is asked about. *trust.Store
// implements it.
type Trust interface {
	// Get returns the peer record, including a revoked one.
	Get(ctx context.Context, id model.DeviceID) (trust.Peer, error)
}

var _ Trust = (*trust.Store)(nil)

// Sink writes an incoming file. The service reads the payload from the blob
// store and hands the reader over; the file name is already sanitized.
type Sink interface {
	// Write stores one file and returns where it went. The path is shown to the
	// user and may be empty.
	Write(ctx context.Context, file File, src io.Reader) (string, error)
}

// EventSink receives the transfer events.
type EventSink interface {
	// Report delivers one event. It runs on the goroutines of the service and
	// must not block.
	Report(ctx context.Context, event Event)
}

// Options configures New.
type Options struct {
	// Blobs stores the payloads of the transfers. It is required.
	Blobs *blob.Store
	// Trust decides what a peer may do. It is required.
	Trust Trust
	// Fetcher downloads the offered payloads. It is required.
	Fetcher *transfer.Fetcher
	// Settings holds the downloads directory. The user downloads directory is
	// used when nil.
	Settings *store.Settings
	// Downloads is the directory of received files. It overrides the setting
	// when it is not empty.
	Downloads string
	// Sink writes received files. The downloads directory is used when nil.
	Sink Sink
	// Events receives the transfer events. Nothing is reported when nil.
	Events EventSink
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// PendingTTL is how long an offer waits for a decision. The package
	// default is used when zero.
	PendingTTL time.Duration
	// Logger receives service events. Nothing is logged when nil.
	Logger *slog.Logger
}

// Service transfers files with the paired devices. It answers offers, runs the
// downloads and serves the offered blobs.
type Service struct {
	blobs     *blob.Store
	trust     Trust
	fetcher   *transfer.Fetcher
	settings  *store.Settings
	downloads string
	sink      Sink
	events    EventSink
	clock     ports.Clock
	log       *slog.Logger
	serve     *transfer.Service
	// pendingTTL is how long an incoming offer waits for a decision.
	pendingTTL time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	sessions map[model.DeviceID]*session.Session
	records  map[string]*record
	order    []string
}

// record is one transfer with what the service needs to continue it.
type record struct {
	transfer Transfer
	// session is the live session with the peer, nil while the peer is
	// offline.
	session *session.Session
	// cancel stops the download of an incoming transfer.
	cancel context.CancelFunc
	// waiting reports an incoming transfer that continues when the peer comes
	// back.
	waiting bool
	// chunks counts the chunks that moved per file hash.
	chunks map[[32]byte]int
	// saved holds the paths of the saved files per file hash.
	saved map[[32]byte]string
	// wake releases the download of an incoming transfer that waits for the
	// peer.
	wake chan struct{}
	// running reports a download goroutine that owns the transfer.
	running bool
	// attempts counts the failed downloads since the last session.
	attempts int
}

// New creates the share service.
func New(opts Options) (*Service, error) {
	switch {
	case opts.Blobs == nil:
		return nil, errors.New("share: blob store is required")
	case opts.Trust == nil:
		return nil, errors.New("share: trust store is required")
	case opts.Fetcher == nil:
		return nil, errors.New("share: blob fetcher is required")
	}

	clock := opts.Clock
	if clock == nil {
		clock = ports.SystemClock
	}
	pendingTTL := opts.PendingTTL
	if pendingTTL <= 0 {
		pendingTTL = PendingTTL
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		blobs:      opts.Blobs,
		trust:      opts.Trust,
		fetcher:    opts.Fetcher,
		settings:   opts.Settings,
		downloads:  opts.Downloads,
		sink:       opts.Sink,
		events:     opts.Events,
		clock:      clock,
		log:        log,
		pendingTTL: pendingTTL,
		ctx:        ctx,
		cancel:     cancel,
		sessions:   make(map[model.DeviceID]*session.Session),
		records:    make(map[string]*record),
	}
	if s.sink == nil {
		s.sink = downloadSink{dir: s.downloadsDir}
	}
	go s.expireLoop()

	serve, err := transfer.NewService(transfer.ServiceOptions{
		Blobs:  opts.Blobs,
		Share:  s,
		Logger: log,
		Served: s.served,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	s.serve = serve
	return s, nil
}

// expireLoop stops the offers that wait for a decision for too long.
func (s *Service) expireLoop() {
	defer observability.Recover()
	interval := min(max(s.pendingTTL/4, 100*time.Millisecond), time.Minute)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.expirePending(s.ctx)
		}
	}
}

// expiredOffer is an offer that stopped waiting for a decision.
type expiredOffer struct {
	transfer Transfer
	sess     *session.Session
}

// expirePending declines the offers that waited longer than the pending TTL and
// tells the senders when they are still connected.
func (s *Service) expirePending(ctx context.Context) {
	s.mu.Lock()
	var expired []expiredOffer
	for _, rec := range s.records {
		if rec.transfer.State != StatePending {
			continue
		}
		if s.clock.Now().Sub(rec.transfer.CreatedAt) < s.pendingTTL {
			continue
		}
		rec.transfer.State = StateDeclined
		rec.transfer.ExpiresAt = time.Time{}
		expired = append(expired, expiredOffer{transfer: rec.transfer.clone(), sess: rec.session})
	}
	s.mu.Unlock()

	for _, offer := range expired {
		if offer.sess != nil && !closed(offer.sess) {
			if err := s.send(ctx, offer.sess, decisionMessage(offer.transfer.ID, false)); err != nil {
				s.log.Debug("sending the expiry failed", "transfer", offer.transfer.ID, "error", err)
			}
		}
		s.log.Info("an offer expired", "transfer", offer.transfer.ID, "peer", offer.transfer.Peer)
		s.report(ctx, Event{Kind: EventDeclined, Peer: offer.transfer.Peer, Transfer: offer.transfer})
	}
}

// Close stops the downloads and releases the goroutines. It is safe to call
// more than once.
func (s *Service) Close() {
	s.cancel()
}

// Register installs the share handlers on a session. The handlers serve the
// offers of the peer, receive its decisions and serve the blobs it asks for.
// The session has to be registered before it opens. The service also follows
// the session events, which is how it learns that a peer is back.
func (s *Service) Register(sess *session.Session) error {
	peer := sess.DeviceID()
	if err := sess.Handle(session.KindShareOffer, func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return s.handleOffer(ctx, sess, msg.GetShareOffer())
	}); err != nil {
		return err
	}
	if err := sess.Handle(session.KindShareDecision, func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return s.handleDecision(ctx, peer, msg.GetShareDecision())
	}); err != nil {
		return err
	}
	if err := sess.Handle(session.KindShareCancel, func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return s.handleCancel(ctx, peer, msg.GetShareCancel())
	}); err != nil {
		return err
	}
	if err := sess.Handle(session.KindShareResult, func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return s.handleResult(ctx, peer, msg.GetShareResult())
	}); err != nil {
		return err
	}
	if err := sess.HandleRequest(PayloadRequest, trust.ActionNone, func(ctx context.Context, stream *session.Request) error {
		return s.serve.HandleRequest(ctx, peer, stream)
	}); err != nil {
		return err
	}
	go s.watch(sess)
	return nil
}

// watch attaches a session when it opens and detaches it when it ends.
func (s *Service) watch(sess *session.Session) {
	defer observability.Recover()
	events := sess.Events()
	for {
		select {
		case <-sess.Done():
			s.detach(sess)
			return
		case event, ok := <-events:
			if !ok {
				s.detach(sess)
				return
			}
			if event.Kind == session.EventOpened {
				s.attach(sess)
			}
		}
	}
}

// attach stores the session of a peer and continues the transfers that waited
// for it.
func (s *Service) attach(sess *session.Session) {
	peer := sess.DeviceID()

	s.mu.Lock()
	s.sessions[peer] = sess
	var resume []string
	for id, rec := range s.records {
		if rec.transfer.Peer != peer || rec.transfer.State.ended() {
			continue
		}
		rec.session = sess
		rec.attempts = 0
		switch {
		case rec.waiting:
			rec.waiting = false
			wakeUp(rec)
			if !rec.running {
				resume = append(resume, id)
			}
		case rec.transfer.Direction == DirectionOutgoing && rec.transfer.State == StateOffered:
			resume = append(resume, id)
		}
	}
	s.mu.Unlock()

	if len(resume) > 0 {
		s.log.Info("resuming transfers", "peer", peer, "transfers", len(resume))
	}
	for _, id := range resume {
		go s.resume(id)
	}
}

// wakeUp releases the download of a transfer that waits for its peer.
func wakeUp(rec *record) {
	select {
	case rec.wake <- struct{}{}:
	default:
	}
}

// detach forgets the session of a peer.
func (s *Service) detach(sess *session.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer := sess.DeviceID()
	if s.sessions[peer] == sess {
		delete(s.sessions, peer)
	}
	for _, rec := range s.records {
		if rec.session == sess {
			rec.session = nil
		}
	}
}

// resume continues a transfer after the peer came back.
func (s *Service) resume(id string) {
	defer observability.Recover()
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State.ended() {
		s.mu.Unlock()
		return
	}
	transfer := rec.transfer.clone()
	direction := transfer.Direction
	s.mu.Unlock()

	if direction == DirectionIncoming {
		s.download(id)
		return
	}
	if err := s.sendOffer(transfer.Peer, transfer); err != nil {
		s.log.Debug("resending the offer failed", "transfer", id, "error", err)
	}
}

// session returns the open session with the peer, or nil.
func (s *Service) session(peer model.DeviceID) *session.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[peer]
	if sess == nil {
		return nil
	}
	select {
	case <-sess.Done():
		return nil
	default:
		return sess
	}
}

// Peers returns the devices with an open session.
func (s *Service) Peers() []model.DeviceID {
	s.mu.Lock()
	defer s.mu.Unlock()
	peers := make([]model.DeviceID, 0, len(s.sessions))
	for id, sess := range s.sessions {
		select {
		case <-sess.Done():
			continue
		default:
			peers = append(peers, id)
		}
	}
	return peers
}

// Transfers returns the transfers, newest first.
func (s *Service) Transfers() []Transfer {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Transfer, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		rec := s.records[s.order[i]]
		if rec == nil {
			continue
		}
		out = append(out, rec.transfer.clone())
	}
	return out
}

// Transfer returns one transfer.
func (s *Service) Transfer(id string) (Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[id]
	if rec == nil {
		return Transfer{}, ErrNotFound
	}
	return rec.transfer.clone(), nil
}

// Requests returns the incoming offers that wait for a decision, newest first.
func (s *Service) Requests() []Transfer {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Transfer, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		rec := s.records[s.order[i]]
		if rec == nil || rec.transfer.Direction != DirectionIncoming || rec.transfer.State != StatePending {
			continue
		}
		out = append(out, rec.transfer.clone())
	}
	return out
}

// Decide accepts or declines an incoming offer that waits for a decision.
func (s *Service) Decide(ctx context.Context, id string, accept bool) error {
	if accept {
		return s.Accept(ctx, id)
	}
	return s.Reject(ctx, id)
}

// Accept accepts an incoming offer that waits for a decision and starts the
// download.
func (s *Service) Accept(ctx context.Context, id string) error {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	if rec.transfer.Direction != DirectionIncoming || rec.transfer.State != StatePending {
		s.mu.Unlock()
		return ErrNotPending
	}
	files := append([]File(nil), rec.transfer.Files...)
	sess := rec.session
	s.mu.Unlock()

	if sess == nil || closed(sess) {
		return ErrPeerOffline
	}
	if err := s.checkOffer(ctx, files); err != nil {
		return err
	}

	s.mu.Lock()
	rec = s.records[id]
	if rec == nil || rec.transfer.State != StatePending {
		s.mu.Unlock()
		return ErrNotPending
	}
	rec.transfer.State = StateTransferring
	rec.transfer.ExpiresAt = time.Time{}
	snapshot := rec.transfer.clone()
	peer := rec.transfer.Peer
	s.mu.Unlock()

	if err := s.send(ctx, sess, decisionMessage(id, true)); err != nil {
		s.fail(ctx, id, fmt.Errorf("share: send the decision: %w", err))
		return err
	}
	s.report(ctx, Event{Kind: EventAccepted, Peer: peer, Transfer: snapshot})
	go func() {
		defer observability.Recover()
		s.download(id)
	}()
	return nil
}

// Reject declines an incoming offer that waits for a decision.
func (s *Service) Reject(ctx context.Context, id string) error {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	if rec.transfer.Direction != DirectionIncoming || rec.transfer.State != StatePending {
		s.mu.Unlock()
		return ErrNotPending
	}
	rec.transfer.State = StateDeclined
	rec.transfer.ExpiresAt = time.Time{}
	snapshot := rec.transfer.clone()
	sess := rec.session
	peer := rec.transfer.Peer
	s.mu.Unlock()

	if sess != nil {
		if err := s.send(ctx, sess, decisionMessage(id, false)); err != nil {
			s.log.Debug("sending the rejection failed", "transfer", id, "error", err)
		}
	}
	s.report(ctx, Event{Kind: EventDeclined, Peer: peer, Transfer: snapshot})
	return nil
}

// Cancel stops a transfer. Both sides stop moving the files and the receiver
// drops what it has received so far.
func (s *Service) Cancel(ctx context.Context, id string) error {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	if rec.transfer.State.ended() {
		s.mu.Unlock()
		return ErrEnded
	}
	pending := rec.transfer.State == StatePending
	incoming := rec.transfer.Direction == DirectionIncoming
	sess := rec.session
	peer := rec.transfer.Peer
	rec.transfer.State = StateCanceled
	rec.transfer.ExpiresAt = time.Time{}
	if rec.cancel != nil {
		rec.cancel()
		rec.cancel = nil
	}
	snapshot := rec.transfer.clone()
	s.mu.Unlock()

	switch {
	case sess == nil:
	case pending:
		if err := s.send(ctx, sess, decisionMessage(id, false)); err != nil {
			s.log.Debug("sending the rejection failed", "transfer", id, "error", err)
		}
	default:
		if err := s.send(ctx, sess, cancelMessage(id)); err != nil {
			s.log.Debug("sending the cancellation failed", "transfer", id, "error", err)
		}
	}
	if incoming {
		s.dropPartials(ctx, snapshot.Files)
	}
	s.report(ctx, Event{Kind: EventCanceled, Peer: peer, Transfer: snapshot})
	return nil
}

// AllowBlob reports whether peer may fetch blob hash of the share transferID.
// It implements transfer.ShareAuthorizer.
func (s *Service) AllowBlob(_ context.Context, peer model.DeviceID, transferID string, hash [32]byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.records[transferID]
	if rec == nil || rec.transfer.Direction != DirectionOutgoing || rec.transfer.Peer != peer {
		return false
	}
	switch rec.transfer.State {
	case StateOffered, StateTransferring:
	default:
		return false
	}
	for _, file := range rec.transfer.Files {
		if file.Hash == hash {
			return true
		}
	}
	return false
}

// checkOffer reports the files of an offer that cannot be received.
func (s *Service) checkOffer(ctx context.Context, files []File) error {
	limit, err := s.blobs.Limit(ctx)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := checkFile(file); err != nil {
			return err
		}
		if file.Size > limit {
			return fmt.Errorf("%w: %d bytes, limit is %d bytes", blob.ErrTooLarge, file.Size, limit)
		}
	}
	return nil
}

// dropPartials removes the blobs of a canceled transfer that are incomplete.
func (s *Service) dropPartials(ctx context.Context, files []File) {
	for _, file := range files {
		info, err := s.blobs.Stat(ctx, file.Hash)
		if err != nil || !info.Partial {
			continue
		}
		if err := s.blobs.Remove(ctx, file.Hash); err != nil {
			s.log.Warn("dropping a partial blob failed", "hash", file.Hash, "error", err)
		}
	}
}

// downloadsDir returns the directory that receives shared files.
func (s *Service) downloadsDir(ctx context.Context) (string, error) {
	if s.downloads != "" {
		return s.downloads, nil
	}
	if s.settings == nil {
		return DefaultDownloadsDir(), nil
	}
	dir, err := s.settings.GetString(ctx, SettingDownloadsDir, "")
	if err != nil || dir == "" {
		return DefaultDownloadsDir(), err
	}
	return dir, nil
}

// send writes one control message to a peer.
func (s *Service) send(ctx context.Context, sess *session.Session, msg *wirev1.ControlMessage) error {
	if err := sess.Send(ctx, msg); err != nil {
		return fmt.Errorf("share: send %w", err)
	}
	return nil
}

// add stores a new record.
func (s *Service) add(rec *record) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records[rec.transfer.ID] = rec
	s.order = append(s.order, rec.transfer.ID)
	s.trim()
}

// drop forgets a record.
func (s *Service) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	for i, known := range s.order {
		if known == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// trim drops the oldest records above the limits, finished ones first.
func (s *Service) trim() {
	for len(s.order) > MaxTransfers {
		index := -1
		for i, id := range s.order {
			if rec := s.records[id]; rec != nil && rec.transfer.State.ended() {
				index = i
				break
			}
		}
		if index < 0 {
			return
		}
		delete(s.records, s.order[index])
		s.order = append(s.order[:index], s.order[index+1:]...)
	}
}

// pendingCountLocked counts the offers that wait for a decision. The caller
// holds the lock.
func (s *Service) pendingCountLocked() int {
	count := 0
	for _, rec := range s.records {
		if rec.transfer.State == StatePending {
			count++
		}
	}
	return count
}

// pendingForLocked counts the offers of one peer that wait for a decision. The
// caller holds the lock.
func (s *Service) pendingForLocked(peer model.DeviceID) int {
	count := 0
	for _, rec := range s.records {
		if rec.transfer.State == StatePending && rec.transfer.Peer == peer {
			count++
		}
	}
	return count
}

// recalc updates the number of bytes that moved.
func recalc(rec *record) {
	var moved uint64
	for _, file := range rec.transfer.Files {
		size := uint64(max(file.Size, 0))
		bytes := uint64(max(rec.chunks[file.Hash], 0)) * blob.ChunkSize
		moved += min(bytes, size)
	}
	rec.transfer.Bytes = min(moved, rec.transfer.TotalBytes)
}

// report delivers an event to the sink.
func (s *Service) report(ctx context.Context, event Event) {
	if s.events == nil {
		return
	}
	s.events.Report(ctx, event)
}

// checkID validates the transfer ID of a request.
func checkID(id string) (string, error) {
	if err := checkTransferID(id); err != nil {
		return "", err
	}
	return id, nil
}
