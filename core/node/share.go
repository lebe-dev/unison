package node

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/trust"
)

// shareEvents publishes the file transfer events.
type shareEvents struct {
	node *Node
}

var _ share.EventSink = shareEvents{}

// Report delivers one transfer event to the subscribers.
func (s shareEvents) Report(ctx context.Context, event share.Event) {
	if event.Kind == share.EventDone && event.Transfer.Direction == share.DirectionIncoming {
		s.node.recordSharedTransfer(ctx, event.Transfer)
	}
	s.node.publish(Event{
		Kind:     EventTransfer,
		Peer:     event.Peer,
		Transfer: event.Transfer,
	})
}

// sharePolicy applies the network policy of the node to the file offers.
type sharePolicy struct {
	node *Node
}

var _ share.Policy = sharePolicy{}

// DecideShare returns the decision of the network policy for an offer. Files
// above the limit of the network wait for the user, like a metadata item of
// the clipboard.
func (p sharePolicy) DecideShare(ctx context.Context, peer model.DeviceID, size int64) (trust.Decision, error) {
	decider := p.node.policy
	if decider == nil {
		return trust.Confirm, errors.New("node: policy is not ready")
	}
	result, err := decider.DecideShare(ctx, peer, size)
	if err != nil {
		return trust.Confirm, err
	}
	switch result.Action {
	case policy.ActionAutoApply:
		return trust.Allow, nil
	case policy.ActionReject:
		return trust.Deny, nil
	default:
		return trust.Confirm, nil
	}
}

// openShare creates the file transfer service of the node.
func (n *Node) openShare(db *store.Store) error {
	service, err := share.New(share.Options{
		Blobs:      n.blobs,
		Trust:      n.trust,
		Fetcher:    n.fetcher,
		Policy:     sharePolicy{node: n},
		Settings:   db.Settings(),
		Downloads:  n.cfg.ShareDownloads,
		Sink:       n.cfg.ShareSink,
		Events:     shareEvents{node: n},
		Clock:      n.clock,
		PendingTTL: n.cfg.SharePendingTTL,
		Logger:     n.log,
	})
	if err != nil {
		return err
	}
	n.share = service
	return nil
}

// installShare registers the file transfer handlers on a new session.
func (n *Node) installShare(_ context.Context, sess *session.Session) error {
	return n.share.Register(sess)
}

// Share returns the file transfer service of the node. It is nil before Start.
func (n *Node) Share() *share.Service {
	return n.share
}

// recordSharedTransfer stores a completed incoming file transfer in history so
// it appears on the dashboard and history views.
func (n *Node) recordSharedTransfer(ctx context.Context, transfer share.Transfer) {
	if n.history == nil || len(transfer.Files) == 0 {
		return
	}

	kind, reps := n.shareHistoryEntry(ctx, transfer.Files)
	if len(reps) == 0 {
		return
	}

	var id model.ClipID
	if _, err := rand.Read(id[:]); err != nil {
		n.log.Warn("allocating history id for shared transfer failed", "transfer", transfer.ID, "error", err)
		return
	}

	now := n.clock.Now()
	ttl := n.TTLs().For(kind)
	if ttl <= 0 {
		ttl = history.DefaultTTLs().For(kind)
	}

	item := model.ClipItem{
		ID:              id,
		CreatedAt:       now,
		ExpiresAt:       now.Add(ttl),
		Kind:            kind,
		Representations: reps,
		Source:          model.SourceShare,
	}
	if _, err := n.history.AppendLocal(ctx, item); err != nil {
		n.log.Warn("recording shared transfer in history failed", "transfer", transfer.ID, "error", err)
	}
}

// shareHistoryEntry maps received share files to a history kind and its
// representations. A single decodable image within clip limits is recorded as
// an image item; all other transfers are recorded as files.
func (n *Node) shareHistoryEntry(ctx context.Context, files []share.File) (model.Kind, []model.Representation) {
	if len(files) == 1 {
		if mediaType, ok := n.shareImageMIME(ctx, files[0]); ok {
			return model.KindImage, []model.Representation{{
				MIME:     mediaType,
				Size:     uint64(max(files[0].Size, 0)),
				Hash:     files[0].Hash,
				FileName: files[0].Name,
			}}
		}
	}

	limit := min(len(files), history.MaxRepresentations)
	reps := make([]model.Representation, 0, limit)
	for _, file := range files[:limit] {
		if file.Hash == ([32]byte{}) {
			continue
		}
		reps = append(reps, model.Representation{
			MIME:     clip.MIMEUnisonFile,
			Size:     uint64(max(file.Size, 0)),
			Hash:     file.Hash,
			FileName: file.Name,
		})
	}
	return model.KindFiles, reps
}

// shareImageMIME reports the normalized image MIME type when a shared file can
// be decoded as an image entry by clip.Thumbnail.
func (n *Node) shareImageMIME(ctx context.Context, file share.File) (string, bool) {
	if file.Hash == ([32]byte{}) || file.Size <= 0 || file.Size > clip.MaxImageBytes {
		return "", false
	}
	raw := strings.TrimSpace(file.MIME)
	if raw == "" {
		raw = mime.TypeByExtension(filepath.Ext(file.Name))
	}
	if raw == "" {
		return "", false
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", false
	}
	normalized := strings.ToLower(mediaType)
	switch normalized {
	case "image/png", "image/jpeg", "image/gif":
	default:
		return "", false
	}
	if n.blobs == nil {
		return "", false
	}
	reader, err := n.blobs.Open(ctx, file.Hash)
	if err != nil {
		return "", false
	}
	defer func() { _ = reader.Close() }()
	if !clip.CanDecodeImage(io.NewSectionReader(reader, 0, reader.Size()), reader.Size()) {
		return "", false
	}
	return normalized, true
}
