package history

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/session"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Exchange limits.
const (
	// MaxBatchItems is the largest number of entries of one batch.
	MaxBatchItems = 64
	// maxBatchBytes bounds the entries of one batch. Every item also has the
	// overhead of the control message.
	maxBatchBytes = 512 << 10
	// MaxSyncItems bounds the entries one exchange records or sends.
	MaxSyncItems = 4096
	// maxSyncBytes bounds the entries one exchange sends.
	maxSyncBytes = 16 << 20
	// maxVectorOrigins bounds the entries of a vector taken from the network.
	maxVectorOrigins = 1024
)

// Sender writes control messages to one peer. *session.Session implements it.
type Sender interface {
	// DeviceID returns the authenticated device.
	DeviceID() model.DeviceID
	// Role returns the authenticated role of the device.
	Role() model.Role
	// Send writes one control message.
	Send(ctx context.Context, msg *wirev1.ControlMessage) error
}

// Register installs the history handlers on a session. A peer that may not
// sync history is left without them.
func (s *Store) Register(sess *session.Session) error {
	if !syncAllowed(sess.Role()) {
		return nil
	}
	if err := sess.Handle(session.KindHistorySync, func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return s.handleSync(ctx, sess, msg.GetHistorySync())
	}); err != nil {
		return err
	}
	return sess.Handle(session.KindHistoryBatch, func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return s.handleBatch(ctx, sess, msg.GetHistoryBatch())
	})
}

// Sync sends the version vector of this device to the peer. The peer answers
// with the entries this device misses.
func (s *Store) Sync(ctx context.Context, peer Sender) error {
	if !syncAllowed(peer.Role()) {
		return nil
	}
	vector, err := s.Vector(ctx)
	if err != nil {
		return err
	}
	s.resetBudget(peer.DeviceID())
	return peer.Send(ctx, syncMessage(vector))
}

// Vector returns the highest sequence number this device holds per origin.
func (s *Store) Vector(ctx context.Context) (map[model.DeviceID]uint64, error) {
	vector := make(map[model.DeviceID]uint64)

	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT origin, seq FROM history_vectors`)
		if err != nil {
			return fmt.Errorf("history: read versions: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				origin []byte
				seq    int64
			)
			if err := rows.Scan(&origin, &seq); err != nil {
				return fmt.Errorf("history: read version: %w", err)
			}
			device, err := model.DeviceIDFromBytes(origin)
			if err != nil {
				return fmt.Errorf("history: stored version: %w", err)
			}
			if seq > 0 {
				vector[device] = uint64(seq)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return vector, nil
}

// handleSync answers a version vector with the entries the peer misses. The
// batches go out on their own goroutine, so the control stream keeps being read
// while they are written: two devices that answer each other at the same time
// would otherwise block on their writes.
func (s *Store) handleSync(ctx context.Context, peer Sender, msg *wirev1.HistorySync) error {
	if !syncAllowed(peer.Role()) || msg == nil {
		return nil
	}
	their, err := vectorFromProto(msg.GetVector())
	if err != nil {
		return fmt.Errorf("history: peer vector: %w", err)
	}
	if !s.beginSend(peer.DeviceID()) {
		s.log.Warn("history exchange already runs for the peer", "peer", peer.DeviceID())
		return nil
	}

	go func() {
		defer s.endSend(peer.DeviceID())
		if err := s.sendMissing(ctx, peer, their); err != nil && ctx.Err() == nil {
			s.log.Debug("history exchange failed", "peer", peer.DeviceID(), "error", err)
		}
	}()
	return nil
}

// handleBatch records the entries a peer sent.
func (s *Store) handleBatch(ctx context.Context, peer Sender, batch *wirev1.HistoryBatch) error {
	if !syncAllowed(peer.Role()) || batch == nil {
		return nil
	}
	items := batch.GetItems()
	if len(items) == 0 {
		return nil
	}
	if len(items) > MaxBatchItems {
		s.log.Warn("history batch is larger than a batch",
			"peer", peer.DeviceID(), "items", len(items), "limit", MaxBatchItems)
		items = items[:MaxBatchItems]
	}

	accepted := s.allowIncoming(peer.DeviceID(), len(items))
	if accepted < len(items) {
		s.log.Warn("history batch cut at the exchange limit",
			"peer", peer.DeviceID(), "items", len(items), "accepted", accepted)
	}
	for _, raw := range items[:accepted] {
		item, err := protoconv.ClipItemFromProto(raw)
		if err != nil {
			s.log.Warn("history entry dropped", "peer", peer.DeviceID(), "error", err)
			continue
		}
		if err := s.Append(ctx, item); err != nil {
			s.log.Warn("history entry not recorded", "peer", peer.DeviceID(), "error", err)
		}
	}
	return nil
}

// sendMissing sends the entries the peer misses, oldest first and in batches.
// An exchange that reaches its limit leaves the rest to the next one, which
// continues from the version vector of the peer.
func (s *Store) sendMissing(ctx context.Context, peer Sender, their map[model.DeviceID]uint64) error {
	versions, err := s.versions(ctx)
	if err != nil {
		return err
	}

	var (
		batch      []*wirev1.ClipItem
		batchBytes int
		sent       int
		sentBytes  int
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := peer.Send(ctx, batchMessage(batch))
		batch = make([]*wirev1.ClipItem, 0, MaxBatchItems)
		batchBytes = 0
		return err
	}

	for _, version := range versions {
		from := their[version.origin]
		if version.seq <= from {
			continue
		}
		entries, err := s.entriesAfter(ctx, version.origin, from, MaxSyncItems-sent+1)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			msg := protoconv.ClipItemToProto(entry)
			size := proto.Size(msg)
			if len(batch) > 0 && (len(batch) >= MaxBatchItems || batchBytes+size > maxBatchBytes) {
				if err := flush(); err != nil {
					return err
				}
			}
			batch = append(batch, msg)
			batchBytes += size
			sent++
			sentBytes += size
			if sent >= MaxSyncItems || sentBytes >= maxSyncBytes {
				s.log.Info("history exchange reached its limit",
					"peer", peer.DeviceID(), "items", sent, "bytes", sentBytes)
				return flush()
			}
		}
	}
	return flush()
}

// version is the highest sequence number of one origin.
type version struct {
	origin model.DeviceID
	seq    uint64
}

// versions returns the version vector ordered by device.
func (s *Store) versions(ctx context.Context) ([]version, error) {
	vector, err := s.Vector(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]version, 0, len(vector))
	for origin, seq := range vector {
		versions = append(versions, version{origin: origin, seq: seq})
	}
	slices.SortFunc(versions, func(a, b version) int {
		return bytes.Compare(a.origin[:], b.origin[:])
	})
	return versions, nil
}

// entriesAfter returns the entries of origin above seq, oldest first.
func (s *Store) entriesAfter(ctx context.Context, origin model.DeviceID, seq uint64, limit int) ([]model.ClipItem, error) {
	if limit <= 0 {
		return nil, nil
	}
	const query = `
		SELECT origin, seq, id, created_at, expires_at, kind, source, deleted, payload
		FROM history_items
		WHERE origin = ? AND seq > ?
		ORDER BY seq
		LIMIT ?`

	var entries []model.ClipItem
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, origin[:], int64(seq), limit)
		if err != nil {
			return fmt.Errorf("history: read entries: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			item, sealed, err := s.scan(rows)
			if err != nil {
				return err
			}
			reps, err := s.openPayload(item.Origin, item.Seq, sealed)
			if err != nil {
				return err
			}
			item.Representations = reps
			entries = append(entries, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// syncMessage wraps a vector in a control message.
func syncMessage(vector map[model.DeviceID]uint64) *wirev1.ControlMessage {
	return &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_HistorySync{
			HistorySync: &wirev1.HistorySync{Vector: vectorToProto(vector)},
		},
	}
}

// batchMessage wraps entries in a control message.
func batchMessage(items []*wirev1.ClipItem) *wirev1.ControlMessage {
	return &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_HistoryBatch{
			HistoryBatch: &wirev1.HistoryBatch{Items: items},
		},
	}
}

// vectorToProto converts a version vector for the wire.
func vectorToProto(vector map[model.DeviceID]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(vector))
	for origin, seq := range vector {
		out[origin.String()] = seq
	}
	return out
}

// vectorFromProto reads a version vector from the wire, ignoring unknown keys.
func vectorFromProto(vector map[string]uint64) (map[model.DeviceID]uint64, error) {
	out := make(map[model.DeviceID]uint64, len(vector))
	read := 0
	for key, seq := range vector {
		if read >= maxVectorOrigins {
			break
		}
		device, err := model.ParseDeviceID(key)
		if err != nil {
			return nil, err
		}
		read++
		if seq > 0 {
			out[device] = seq
		}
	}
	return out, nil
}

// syncAllowed reports whether the role takes part in the history exchange.
func syncAllowed(role model.Role) bool {
	return role == model.RolePersonal
}

// allowIncoming returns how many entries of the peer the current exchange may
// still record.
func (s *Store) allowIncoming(peer model.DeviceID, items int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	left := MaxSyncItems - s.budgets[peer]
	if left <= 0 {
		return 0
	}
	if items > left {
		items = left
	}
	s.budgets[peer] += items
	return items
}

// resetBudget starts a new exchange with the peer.
func (s *Store) resetBudget(peer model.DeviceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.budgets, peer)
}

// beginSend reserves the peer for one outgoing exchange.
func (s *Store) beginSend(peer model.DeviceID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sending[peer] {
		return false
	}
	s.sending[peer] = true
	return true
}

// endSend releases the peer.
func (s *Store) endSend(peer model.DeviceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sending, peer)
}
