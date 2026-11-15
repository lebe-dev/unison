package transfer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// ServiceOptions configures NewService.
type ServiceOptions struct {
	// Blobs is the store the service reads blobs from. It is required.
	Blobs *blob.Store
	// Share authorizes share blobs by their transfer ID.
	Share ShareAuthorizer
	// FrameTimeout bounds one frame the service writes. DefaultFrameTimeout is
	// used when zero.
	FrameTimeout time.Duration
	// Logger receives transfer events. Nothing is logged when nil.
	Logger *slog.Logger
}

// Service serves blobs to peers.
type Service struct {
	blobs   *blob.Store
	share   ShareAuthorizer
	timeout time.Duration
	log     *slog.Logger
}

// NewService prepares a blob service.
func NewService(opts ServiceOptions) (*Service, error) {
	if opts.Blobs == nil {
		return nil, errors.New("transfer: blob store is required")
	}
	s := &Service{
		blobs:   opts.Blobs,
		share:   opts.Share,
		timeout: opts.FrameTimeout,
		log:     opts.Logger,
	}
	if s.timeout <= 0 {
		s.timeout = DefaultFrameTimeout
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// Register registers the handlers of the service on a session: availability
// queries and blob request streams. The streams need the fetch-blob capability
// of the peer.
func (s *Service) Register(sess *session.Session) error {
	if err := s.RegisterQueries(sess); err != nil {
		return err
	}
	return s.RegisterRequests(sess)
}

// RegisterQueries registers the blob availability query handler on a session.
func (s *Service) RegisterQueries(sess *session.Session) error {
	peer := sess.DeviceID()
	return sess.Handle(session.KindHaveBlob, func(ctx context.Context, msg *wirev1.ControlMessage) error {
		return s.HandleHaveBlob(ctx, peer, sess, msg.GetHaveBlob())
	})
}

// RegisterRequests registers the blob request stream handler on a session.
func (s *Service) RegisterRequests(sess *session.Session) error {
	peer := sess.DeviceID()
	return sess.HandleRequest(PayloadRequest, trust.ActionFetchBlob, func(ctx context.Context, stream *session.Request) error {
		return s.HandleRequest(ctx, peer, stream)
	})
}

// HandleHaveBlob answers a blob availability query of peer.
func (s *Service) HandleHaveBlob(ctx context.Context, peer model.DeviceID, sender Sender, msg *wirev1.HaveBlob) error {
	if len(msg.GetHash()) != 32 {
		return fmt.Errorf("%w: hash of %d bytes", ErrRequest, len(msg.GetHash()))
	}
	var hash [32]byte
	copy(hash[:], msg.GetHash())

	resp := &wirev1.HaveBlobResp{Hash: hash[:]}
	info, err := s.blobs.Stat(ctx, hash)
	switch {
	case errors.Is(err, blob.ErrNotFound):
	case err != nil:
		return err
	case !info.Partial:
		resp.Have = true
		resp.Chunks = uint32(info.Chunks)
	}

	s.log.Debug("answered a blob query", "peer", peer, "hash", hashHex(hash), "have", resp.Have)
	return sender.Send(ctx, &wirev1.ControlMessage{Body: &wirev1.ControlMessage_HaveBlobResp{HaveBlobResp: resp}})
}

// HandleRequest serves one blob request stream of peer. The stream stays owned
// by the session, which closes it after this call.
func (s *Service) HandleRequest(ctx context.Context, peer model.DeviceID, stream *session.Request) error {
	msg := &wirev1.BlobRequest{}
	if err := s.read(ctx, stream, msg); err != nil {
		return err
	}
	req, err := parseRequest(stream.Header(), msg)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, peer, req); err != nil {
		s.log.Warn("blob request denied", "peer", peer, "hash", hashHex(req.hash), "error", err)
		return s.refuse(ctx, stream, wirev1.BlobStatus_BLOB_STATUS_DENIED, err)
	}

	info, err := s.blobs.Stat(ctx, req.hash)
	if errors.Is(err, blob.ErrNotFound) {
		return s.refuse(ctx, stream, wirev1.BlobStatus_BLOB_STATUS_MISSING, err)
	}
	if err != nil {
		return err
	}
	if info.Partial {
		return s.refuse(ctx, stream, wirev1.BlobStatus_BLOB_STATUS_UNAVAILABLE, blob.ErrPartial)
	}
	var from, to int
	if !req.treeOnly {
		from, to, err = req.rangeOf(info.Chunks)
		if err != nil {
			return s.refuse(ctx, stream, wirev1.BlobStatus_BLOB_STATUS_UNAVAILABLE, err)
		}
	}

	tree, err := s.blobs.Tree(ctx, req.hash)
	if err != nil {
		return err
	}
	if err := s.write(ctx, stream, treeReply(info, tree)); err != nil {
		return err
	}
	if req.treeOnly {
		return nil
	}

	reader, err := s.blobs.Open(ctx, req.hash)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	for i := from; i <= to; i++ {
		data, err := reader.ReadChunk(i)
		if err != nil {
			return err
		}
		sum := chunkHash(data)
		if err := s.write(ctx, stream, chunkReply(i, data, sum[:])); err != nil {
			return err
		}
	}

	s.log.Info("served blob", "peer", peer, "hash", hashHex(req.hash),
		"from", from, "to", to, "size", info.Size)
	return nil
}

// authorize checks the share a request belongs to. A request without a transfer
// ID is a blob of the history, which the session already allowed.
func (s *Service) authorize(ctx context.Context, peer model.DeviceID, req request) error {
	if req.transfer == "" {
		return nil
	}
	if s.share == nil {
		return fmt.Errorf("%w: share blob without an authorizer", ErrDenied)
	}
	if !s.share.AllowBlob(ctx, peer, req.transfer, req.hash) {
		return fmt.Errorf("%w: share blob", ErrDenied)
	}
	return nil
}

// refuse tells the peer why the request cannot be served.
func (s *Service) refuse(ctx context.Context, stream *session.Request, status wirev1.BlobStatus, err error) error {
	if werr := s.write(ctx, stream, errorReply(status)); werr != nil {
		return werr
	}
	return err
}

// read decodes one frame of the stream.
func (s *Service) read(ctx context.Context, stream *session.Request, msg proto.Message) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return stream.Read(ctx, msg)
}

// write sends one frame of the stream.
func (s *Service) write(ctx context.Context, stream *session.Request, msg proto.Message) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return stream.Write(ctx, msg)
}

// hashHex returns the hexadecimal form of a hash used in logs.
func hashHex(hash [32]byte) string {
	return hex.EncodeToString(hash[:])
}
