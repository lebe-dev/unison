package share

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lebe-dev/unison/core/blob/transfer"
	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// SendPaths stores the files and offers them to the peers. It returns the ID of
// the transfer of every peer that received an offer.
func (s *Service) SendPaths(ctx context.Context, paths []string, peers []model.DeviceID) ([]string, error) {
	switch {
	case len(paths) == 0:
		return nil, ErrNoFiles
	case len(paths) > MaxFiles:
		return nil, ErrTooManyFiles
	case len(peers) == 0:
		return nil, ErrNoPeers
	}
	for _, peer := range peers {
		if s.session(peer) == nil {
			return nil, fmt.Errorf("%w: %s", ErrPeerOffline, peer)
		}
	}

	files := make([]File, 0, len(paths))
	for _, path := range paths {
		file, err := s.store(ctx, path)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return s.Send(ctx, files, peers)
}

// Send offers files that are already in the blob store. It returns the ID of
// the transfer of every peer that received an offer.
func (s *Service) Send(ctx context.Context, files []File, peers []model.DeviceID) ([]string, error) {
	switch {
	case len(files) == 0:
		return nil, ErrNoFiles
	case len(files) > MaxFiles:
		return nil, ErrTooManyFiles
	case len(peers) == 0:
		return nil, ErrNoPeers
	}
	for _, file := range files {
		if err := checkFile(file); err != nil {
			return nil, err
		}
		info, err := s.blobs.Stat(ctx, file.Hash)
		if err != nil {
			return nil, fmt.Errorf("share: read file %q: %w", file.Name, err)
		}
		if info.Partial {
			return nil, fmt.Errorf("%w: %q is incomplete", ErrInvalidFile, file.Name)
		}
		if info.Size != file.Size {
			return nil, fmt.Errorf("%w: %q is %d bytes, want %d", ErrInvalidFile, file.Name, info.Size, file.Size)
		}
	}

	var (
		ids      []string
		failures []error
	)
	for _, peer := range peers {
		id, err := s.offer(ctx, peer, files)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", peer, err))
			continue
		}
		ids = append(ids, id)
	}
	return ids, errors.Join(failures...)
}

// offer creates the transfer of one peer and sends the offer.
func (s *Service) offer(ctx context.Context, peer model.DeviceID, files []File) (string, error) {
	sess := s.session(peer)
	if sess == nil {
		return "", ErrPeerOffline
	}
	id, err := newID()
	if err != nil {
		return "", err
	}

	name := peer.String()
	if record, err := s.trust.Get(ctx, peer); err == nil {
		name = record.Name
	}

	rec := &record{
		transfer: Transfer{
			ID:         id,
			Direction:  DirectionOutgoing,
			Peer:       peer,
			PeerName:   name,
			State:      StateOffered,
			Files:      append([]File(nil), files...),
			TotalBytes: totalBytes(files),
			CreatedAt:  s.clock.Now(),
		},
		session: sess,
		chunks:  make(map[[32]byte]int, len(files)),
		saved:   make(map[[32]byte]string, len(files)),
		wake:    make(chan struct{}, 1),
	}
	snapshot := rec.transfer.clone()
	s.add(rec)

	if err := s.sendOffer(peer, snapshot); err != nil {
		s.drop(id)
		return "", err
	}
	s.log.Info("offered files", "transfer", id, "peer", peer, "files", len(files), "bytes", snapshot.TotalBytes)
	s.report(ctx, Event{Kind: EventOffered, Peer: peer, Transfer: snapshot})
	return id, nil
}

// sendOffer sends the offer of a transfer over the open session of the peer.
func (s *Service) sendOffer(peer model.DeviceID, transfer Transfer) error {
	sess := s.session(peer)
	if sess == nil {
		return ErrPeerOffline
	}
	if err := s.send(s.ctx, sess, offerMessage(transfer)); err != nil {
		return err
	}
	return nil
}

// store puts one path into the blob store.
func (s *Service) store(ctx context.Context, path string) (File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return File{}, fmt.Errorf("share: read %s: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%w: %q is not a regular file", ErrInvalidFile, filepath.Base(path))
	}

	source, err := os.Open(path)
	if err != nil {
		return File{}, fmt.Errorf("share: open %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = source.Close() }()

	stored, err := s.blobs.Put(ctx, source, info.Size())
	if err != nil {
		return File{}, fmt.Errorf("share: store %s: %w", filepath.Base(path), err)
	}
	name := SafeName(filepath.Base(path))
	return File{
		Name:  name,
		Size:  info.Size(),
		Hash:  stored.Hash,
		MTime: info.ModTime(),
		MIME:  mimeOf(name),
	}, nil
}

// handleDecision applies the answer of a peer to an offer of this device.
func (s *Service) handleDecision(ctx context.Context, peer model.DeviceID, msg *wirev1.ShareDecision) error {
	id, err := checkID(msg.GetTransferId())
	if err != nil {
		return err
	}

	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.Direction != DirectionOutgoing || rec.transfer.Peer != peer {
		s.mu.Unlock()
		s.log.Debug("decision for an unknown transfer", "transfer", id, "peer", peer)
		return nil
	}
	if rec.transfer.State != StateOffered {
		s.mu.Unlock()
		return nil
	}
	if msg.GetAccepted() {
		rec.transfer.State = StateTransferring
	} else {
		rec.transfer.State = StateDeclined
	}
	snapshot := rec.transfer.clone()
	s.mu.Unlock()

	kind := EventAccepted
	if !msg.GetAccepted() {
		kind = EventDeclined
	}
	s.report(ctx, Event{Kind: kind, Peer: peer, Transfer: snapshot})
	return nil
}

// handleResult applies the outcome a receiver reports.
func (s *Service) handleResult(ctx context.Context, peer model.DeviceID, msg *wirev1.ShareResult) error {
	id, err := checkID(msg.GetTransferId())
	if err != nil {
		return err
	}

	s.mu.Lock()
	rec := s.records[id]
	if rec == nil || rec.transfer.Direction != DirectionOutgoing || rec.transfer.Peer != peer {
		s.mu.Unlock()
		return nil
	}
	if rec.transfer.State == StateCanceled {
		s.mu.Unlock()
		return nil
	}
	if msg.GetOk() {
		rec.transfer.State = StateDone
		rec.transfer.Bytes = rec.transfer.TotalBytes
	} else {
		rec.transfer.State = StateFailed
		rec.transfer.Err = msg.GetError()
	}
	snapshot := rec.transfer.clone()
	s.mu.Unlock()

	kind := EventDone
	if !msg.GetOk() {
		kind = EventFailed
	}
	s.report(ctx, Event{Kind: kind, Peer: peer, Transfer: snapshot})
	return nil
}

// served counts the chunks the service sends to a receiver.
func (s *Service) served(chunk transfer.Served) {
	if chunk.TransferID == "" {
		return
	}

	s.mu.Lock()
	rec := s.records[chunk.TransferID]
	if rec == nil || rec.transfer.State != StateTransferring {
		s.mu.Unlock()
		return
	}
	count := rec.chunks[chunk.Hash] + 1
	rec.chunks[chunk.Hash] = count
	if count != 1 && count%progressEvery != 0 && count != chunk.Chunks {
		s.mu.Unlock()
		return
	}
	recalc(rec)
	snapshot := rec.transfer.clone()
	peer := rec.transfer.Peer
	s.mu.Unlock()

	s.report(s.ctx, Event{Kind: EventProgress, Peer: peer, Transfer: snapshot})
}
