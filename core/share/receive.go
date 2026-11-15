package share

import (
	"context"
	"fmt"
	"io"

	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// handleOffer decides what happens to an offer of a peer. A Personal peer is
// accepted at once, a Friends peer waits for the local decision and a peer the
// matrix refuses gets a rejection. The payloads are downloaded only after the
// offer is accepted.
func (s *Service) handleOffer(ctx context.Context, sess *session.Session, msg *wirev1.ShareOffer) error {
	peer := sess.DeviceID()
	id, err := checkID(msg.GetTransferId())
	if err != nil {
		return err
	}

	files, err := filesOf(msg.GetFiles())
	if err != nil {
		s.log.Warn("refused a malformed offer", "peer", peer, "error", err)
		return s.send(ctx, sess, decisionMessage(id, false))
	}
	s.expirePending(ctx)

	s.mu.Lock()
	known := s.records[id]
	pending := s.pendingCount()
	s.mu.Unlock()
	if known != nil {
		return s.repeatOffer(ctx, known, sess)
	}

	record, err := s.trust.Get(ctx, peer)
	if err != nil {
		return fmt.Errorf("share: read peer: %w", err)
	}
	name := record.Name
	if name == "" {
		name = peer.String()
	}

	decision := record.Decision(trust.ActionReceiveShare)
	if decision == trust.Allow {
		if err := s.checkOffer(ctx, files); err != nil {
			s.log.Warn("refused an offer", "peer", peer, "error", err)
			return s.send(ctx, sess, decisionMessage(id, false))
		}
		return s.startIncoming(ctx, sess, id, name, files)
	}
	if decision == trust.Confirm && pending < MaxPending {
		return s.queueIncoming(ctx, sess, id, name, files)
	}

	s.log.Info("refused a share offer", "peer", peer, "decision", decision.String())
	return s.send(ctx, sess, decisionMessage(id, false))
}

// repeatOffer answers an offer the receiver already knows, so a sender that
// repeated the offer after a reconnect learns the decision.
func (s *Service) repeatOffer(ctx context.Context, rec *record, sess *session.Session) error {
	if rec.transfer.Direction != DirectionIncoming || rec.transfer.Peer != sess.DeviceID() {
		return nil
	}
	switch rec.transfer.State {
	case StateTransferring, StateDone:
		return s.send(ctx, sess, decisionMessage(rec.transfer.ID, true))
	case StateDeclined:
		return s.send(ctx, sess, decisionMessage(rec.transfer.ID, false))
	default:
		return nil
	}
}

// queueIncoming stores an offer that waits for the local decision.
func (s *Service) queueIncoming(ctx context.Context, sess *session.Session, id, name string, files []File) error {
	rec := &record{
		transfer: Transfer{
			ID:         id,
			Direction:  DirectionIncoming,
			Peer:       sess.DeviceID(),
			PeerName:   name,
			State:      StatePending,
			Files:      files,
			TotalBytes: totalBytes(files),
			CreatedAt:  s.clock.Now(),
		},
		session: sess,
		chunks:  make(map[[32]byte]int, len(files)),
		saved:   make(map[[32]byte]string, len(files)),
		wake:    make(chan struct{}, 1),
	}
	s.add(rec)
	s.log.Info("offer waits for a decision", "peer", rec.transfer.Peer, "transfer", id, "files", len(files))
	s.report(ctx, Event{Kind: EventPending, Peer: rec.transfer.Peer, Transfer: rec.transfer.clone()})
	return nil
}

// startIncoming accepts an offer, tells the sender and starts the download.
func (s *Service) startIncoming(ctx context.Context, sess *session.Session, id, name string, files []File) error {
	rec := &record{
		transfer: Transfer{
			ID:         id,
			Direction:  DirectionIncoming,
			Peer:       sess.DeviceID(),
			PeerName:   name,
			State:      StateTransferring,
			Files:      files,
			TotalBytes: totalBytes(files),
			CreatedAt:  s.clock.Now(),
		},
		session: sess,
		chunks:  make(map[[32]byte]int, len(files)),
		saved:   make(map[[32]byte]string, len(files)),
		wake:    make(chan struct{}, 1),
	}
	s.add(rec)

	if err := s.send(ctx, sess, decisionMessage(id, true)); err != nil {
		s.drop(id)
		return err
	}
	s.log.Info("accepted an offer", "peer", rec.transfer.Peer, "transfer", id, "files", len(files))
	s.report(ctx, Event{Kind: EventAccepted, Peer: rec.transfer.Peer, Transfer: rec.transfer.clone()})
	// The download runs on its own goroutine: the session handler has to return
	// to the control stream.
	go s.download(id)
	return nil
}

// download fetches the files of an accepted transfer and writes them. It runs
// on its own goroutine, waits for the peer after an interruption and continues
// from the chunks that are already stored.
func (s *Service) download(id string) {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring {
		s.mu.Unlock()
		return
	}
	files := append([]File(nil), rec.transfer.Files...)
	ctx, cancel := context.WithCancel(s.ctx)
	rec.cancel = cancel
	s.mu.Unlock()

	// A second goroutine for the same transfer would download the same chunks.
	if !s.claimDownload(id) {
		cancel()
		return
	}
	defer s.releaseDownload(id)

	for _, file := range files {
		if s.savedFile(id, file.Hash) {
			continue
		}
		if !s.receive(ctx, id, file) {
			return
		}
		if err := s.save(ctx, id, file); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.fail(ctx, id, err)
			return
		}
	}
	s.finish(ctx, id)
}

// receive downloads one file and waits for the peer when the session ends. It
// reports false when the transfer stopped.
func (s *Service) receive(ctx context.Context, id string, file File) bool {
	for {
		if err := ctx.Err(); err != nil {
			return false
		}
		sess := s.liveSession(id)
		if sess == nil {
			if !s.awaitPeer(ctx, id) {
				return false
			}
			continue
		}

		err := s.fetch(ctx, id, sess, file)
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		s.log.Debug("download interrupted", "transfer", id, "session-open", !closed(sess), "error", err)
		// A session that ended is an interruption the transfer survives. A
		// session that is still up is retried a few times before the file is
		// reported as failed.
		if closed(sess) {
			if !s.awaitPeer(ctx, id) {
				return false
			}
			continue
		}
		if !s.retry(id) {
			s.fail(ctx, id, err)
			return false
		}
	}
}

// awaitPeer waits until the peer of the transfer has a session again. It
// reports false when the transfer stops while it waits.
func (s *Service) awaitPeer(ctx context.Context, id string) bool {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring {
		s.mu.Unlock()
		return false
	}
	if sess := s.sessions[rec.transfer.Peer]; sess != nil && !closed(sess) {
		rec.session = sess
		s.mu.Unlock()
		return true
	}
	rec.waiting = true
	wake := rec.wake
	s.mu.Unlock()

	s.log.Info("download waits for the peer", "transfer", id)
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	}
}

// retry reports whether a file of the transfer may be downloaded again.
func (s *Service) retry(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring {
		return false
	}
	if rec.attempts >= maxAttempts {
		return false
	}
	rec.attempts++
	return true
}

// claimDownload takes the download of a transfer. Only the first caller gets
// it.
func (s *Service) claimDownload(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring || rec.running {
		return false
	}
	rec.running = true
	return true
}

// releaseDownload frees the download of a transfer.
func (s *Service) releaseDownload(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec := s.records[id]; rec != nil {
		rec.running = false
	}
}

// liveSession returns the open session with the peer of the transfer.
func (s *Service) liveSession(id string) *session.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring {
		return nil
	}
	sess := s.sessions[rec.transfer.Peer]
	if sess == nil || closed(sess) {
		return nil
	}
	return sess
}

// closed reports a session that has ended.
func closed(sess *session.Session) bool {
	select {
	case <-sess.Done():
		return true
	default:
		return false
	}
}

// fetch downloads one file of a transfer from its sender.
func (s *Service) fetch(ctx context.Context, id string, sess *session.Session, file File) error {
	_, err := s.fetcher.Fetch(ctx, transfer.FetchRequest{
		Hash:        file.Hash,
		Peers:       []transfer.Peer{sess},
		TransferID:  id,
		PayloadType: PayloadRequest,
		Trusted:     true,
		Progress: func(progress transfer.Progress) {
			s.progress(id, progress)
		},
	})
	return err
}

// save writes one received file and records where it went.
func (s *Service) save(ctx context.Context, id string, file File) error {
	info, err := s.blobs.Stat(ctx, file.Hash)
	if err != nil {
		return err
	}
	if info.Size != file.Size {
		return fmt.Errorf("%w: %q is %d bytes, the offer says %d", ErrInvalidFile, file.Name, info.Size, file.Size)
	}

	reader, err := s.blobs.Open(ctx, file.Hash)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	path, err := s.sink.Write(ctx, file, io.NewSectionReader(reader, 0, info.Size))
	if err != nil {
		return err
	}

	s.mu.Lock()
	rec := s.records[id]
	if rec == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	rec.saved[file.Hash] = path
	if path != "" {
		rec.transfer.Paths = append(rec.transfer.Paths, path)
	}
	snapshot := rec.transfer.clone()
	peer := rec.transfer.Peer
	s.mu.Unlock()

	s.log.Info("saved a shared file", "transfer", id, "name", file.Name, "size", file.Size)
	s.report(ctx, Event{Kind: EventSaved, Peer: peer, Transfer: snapshot, Path: path})
	return nil
}

// progress records the chunks of a download.
func (s *Service) progress(id string, progress transfer.Progress) {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring {
		s.mu.Unlock()
		return
	}
	rec.chunks[progress.Hash] = progress.Received
	if progress.Received != 1 && progress.Received%progressEvery != 0 && progress.Received != progress.Chunks {
		s.mu.Unlock()
		return
	}
	recalc(rec)
	snapshot := rec.transfer.clone()
	peer := rec.transfer.Peer
	s.mu.Unlock()

	s.report(s.ctx, Event{Kind: EventProgress, Peer: peer, Transfer: snapshot})
}

// finish reports a completed download.
func (s *Service) finish(ctx context.Context, id string) {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring {
		s.mu.Unlock()
		return
	}
	rec.transfer.State = StateDone
	rec.transfer.Bytes = rec.transfer.TotalBytes
	rec.cancel = nil
	snapshot := rec.transfer.clone()
	sess := rec.session
	peer := rec.transfer.Peer
	s.mu.Unlock()

	if sess != nil {
		if err := s.send(ctx, sess, resultMessage(id, true, "")); err != nil {
			s.log.Debug("sending the result failed", "transfer", id, "error", err)
		}
	}
	s.log.Info("received a transfer", "transfer", id, "peer", peer, "files", len(snapshot.Files))
	s.report(ctx, Event{Kind: EventDone, Peer: peer, Transfer: snapshot})
}

// fail reports a transfer that cannot finish and drops what was received.
func (s *Service) fail(ctx context.Context, id string, reason error) {
	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.State != StateTransferring {
		s.mu.Unlock()
		return
	}
	rec.transfer.State = StateFailed
	rec.transfer.Err = reason.Error()
	rec.cancel = nil
	snapshot := rec.transfer.clone()
	sess := rec.session
	peer := rec.transfer.Peer
	s.mu.Unlock()

	s.dropPartials(ctx, snapshot.Files)
	if sess != nil {
		if err := s.send(ctx, sess, resultMessage(id, false, snapshot.Err)); err != nil {
			s.log.Debug("sending the failure failed", "transfer", id, "error", err)
		}
	}
	s.log.Warn("share transfer failed", "transfer", id, "peer", peer, "error", reason)
	s.report(ctx, Event{Kind: EventFailed, Peer: peer, Transfer: snapshot})
}

// savedFile reports whether a file of the transfer is on the disk.
func (s *Service) savedFile(id string, hash [32]byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[id]
	if rec == nil {
		return false
	}
	return rec.saved[hash] != ""
}

// handleCancel stops a transfer the peer stopped.
func (s *Service) handleCancel(ctx context.Context, peer model.DeviceID, msg *wirev1.ShareCancel) error {
	id, err := checkID(msg.GetTransferId())
	if err != nil {
		return err
	}

	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.Peer != peer || rec.transfer.State.ended() {
		s.mu.Unlock()
		return nil
	}
	rec.transfer.State = StateCanceled
	rec.transfer.Err = ""
	if rec.cancel != nil {
		rec.cancel()
		rec.cancel = nil
	}
	rec.waiting = false
	snapshot := rec.transfer.clone()
	s.mu.Unlock()

	if snapshot.Direction == DirectionIncoming {
		s.dropPartials(ctx, snapshot.Files)
	}
	s.log.Info("the peer canceled a transfer", "transfer", id, "peer", peer)
	s.report(ctx, Event{Kind: EventCanceled, Peer: peer, Transfer: snapshot})
	return nil
}
