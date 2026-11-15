package session

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Request is a bidirectional stream that carries one request and its answer.
type Request struct {
	stream  *quic.Stream
	frames  *frameReader
	header  *wirev1.StreamHeader
	writeMu sync.Mutex
}

// Header returns the header the stream was opened with.
func (r *Request) Header() *wirev1.StreamHeader {
	return r.header
}

// Read decodes the next message of the stream. A frame over the stream limit is
// refused before it is allocated.
func (r *Request) Read(ctx context.Context, msg proto.Message) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := r.stream.SetReadDeadline(deadline); err != nil {
			return err
		}
		defer func() { _ = r.stream.SetReadDeadline(time.Time{}) }()
	}
	return r.frames.readMessage(MaxStreamMessage, msg)
}

// Write sends one message on the stream.
func (r *Request) Write(ctx context.Context, msg proto.Message) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		if err := r.stream.SetWriteDeadline(deadline); err != nil {
			return err
		}
		defer func() { _ = r.stream.SetWriteDeadline(time.Time{}) }()
	}
	return writeFrame(r.stream, msg, MaxStreamMessage)
}

// Close closes the stream, which tells the peer that the answer is complete.
func (r *Request) Close() error {
	return r.stream.Close()
}

// CancelRead stops reading the answers of the peer. A stream releases its slot
// of the connection only when both directions are closed.
func (r *Request) CancelRead() {
	r.stream.CancelRead(quic.StreamErrorCode(0))
}

// Transfer is an incoming unidirectional stream that carries one transfer.
type Transfer struct {
	stream *quic.ReceiveStream
	frames *frameReader
	header *wirev1.StreamHeader
}

// Header returns the header the stream was opened with.
func (t *Transfer) Header() *wirev1.StreamHeader {
	return t.header
}

// Read decodes the next message of the stream. A frame over the stream limit is
// refused before it is allocated.
func (t *Transfer) Read(ctx context.Context, msg proto.Message) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := t.stream.SetReadDeadline(deadline); err != nil {
			return err
		}
		defer func() { _ = t.stream.SetReadDeadline(time.Time{}) }()
	}
	return t.frames.readMessage(MaxStreamMessage, msg)
}

// Cancel stops the transfer and tells the peer that the payload is not wanted.
func (t *Transfer) Cancel() {
	t.stream.CancelRead(quic.StreamErrorCode(0))
}

// TransferWriter writes an outgoing unidirectional transfer stream.
type TransferWriter struct {
	stream *quic.SendStream
	header *wirev1.StreamHeader
}

// Header returns the header the stream was opened with.
func (w *TransferWriter) Header() *wirev1.StreamHeader {
	return w.header
}

// Write sends one message on the stream.
func (w *TransferWriter) Write(ctx context.Context, msg proto.Message) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := w.stream.SetWriteDeadline(deadline); err != nil {
			return err
		}
		defer func() { _ = w.stream.SetWriteDeadline(time.Time{}) }()
	}
	return writeFrame(w.stream, msg, MaxStreamMessage)
}

// Close closes the stream, which marks the end of the transfer for the peer.
func (w *TransferWriter) Close() error {
	return w.stream.Close()
}

// OpenRequest opens a request stream with header as its first frame. The session
// sets the stream kind, the caller fills the payload type and the payload
// details.
func (s *Session) OpenRequest(ctx context.Context, header *wirev1.StreamHeader) (*Request, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	hdr, err := streamHeader(header, wirev1.StreamKind_STREAM_KIND_REQUEST)
	if err != nil {
		return nil, err
	}

	stream, err := s.conn.OpenStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("session: open request stream: %w", err)
	}
	frames := newFrameReader(stream)
	if err := s.writeHeader(ctx, stream, hdr); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return &Request{stream: stream, frames: frames, header: hdr}, nil
}

// OpenTransfer opens a transfer stream with header as its first frame. The
// session sets the stream kind, the caller fills the payload type and the
// payload details.
func (s *Session) OpenTransfer(ctx context.Context, header *wirev1.StreamHeader) (*TransferWriter, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	hdr, err := streamHeader(header, wirev1.StreamKind_STREAM_KIND_TRANSFER)
	if err != nil {
		return nil, err
	}

	stream, err := s.conn.OpenUniStream()
	if err != nil {
		return nil, fmt.Errorf("session: open transfer stream: %w", err)
	}
	if err := s.writeHeader(ctx, stream, hdr); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return &TransferWriter{stream: stream, header: hdr}, nil
}

// HandleRequest registers fn for incoming request streams whose header payload
// type is payloadType. The session checks action before fn runs; an action of
// ActionNone leaves the decision to fn, which then has to authorize the stream
// itself.
func (s *Session) HandleRequest(payloadType string, action trust.Action, fn RequestHandler) error {
	if err := validatePayloadType(payloadType); err != nil {
		return err
	}
	if fn == nil {
		return fmt.Errorf("%w: nil request handler", ErrHandler)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if _, ok := s.requests[payloadType]; ok {
		return fmt.Errorf("%w: request type %s is already handled", ErrHandler, payloadType)
	}
	s.requests[payloadType] = requestRoute{action: action, handler: fn}
	return nil
}

// HandleTransfer registers fn for incoming transfer streams whose header
// payload type is payloadType. The session checks action before fn runs; an
// action of ActionNone leaves the decision to fn, which then has to authorize
// the stream itself.
func (s *Session) HandleTransfer(payloadType string, action trust.Action, fn TransferHandler) error {
	if err := validatePayloadType(payloadType); err != nil {
		return err
	}
	if fn == nil {
		return fmt.Errorf("%w: nil transfer handler", ErrHandler)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if _, ok := s.transfers[payloadType]; ok {
		return fmt.Errorf("%w: transfer type %s is already handled", ErrHandler, payloadType)
	}
	s.transfers[payloadType] = transferRoute{action: action, handler: fn}
	return nil
}

// ready reports whether a stream may be opened.
func (s *Session) ready() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case !s.opened:
		return ErrNotOpen
	}
	return nil
}

// acceptRequests accepts the bidirectional streams opened by the peer.
func (s *Session) acceptRequests() {
	for {
		stream, err := s.conn.AcceptStream(s.ctx)
		if err != nil {
			return
		}
		load := s.requestLoad.Add(1)
		if load > MaxRequestStreams {
			s.requestLoad.Add(-1)
			s.violate("too many request streams")
			_ = stream.Close()
			continue
		}
		go s.serveRequest(stream)
	}
}

// acceptTransfers accepts the unidirectional streams opened by the peer.
func (s *Session) acceptTransfers() {
	for {
		stream, err := s.conn.AcceptUniStream(s.ctx)
		if err != nil {
			return
		}
		load := s.transferLoad.Add(1)
		if load > MaxTransferStreams {
			s.transferLoad.Add(-1)
			s.violate("too many transfer streams")
			stream.CancelRead(quic.StreamErrorCode(0))
			continue
		}
		go s.serveTransfer(stream)
	}
}

// serveRequest checks an incoming request stream and runs its handler.
func (s *Session) serveRequest(stream *quic.Stream) {
	defer s.requestLoad.Add(-1)
	// Both directions have to end for the stream to release its slot of the
	// connection: the handler is done with the request, so the rest of the read
	// side is dropped.
	defer func() {
		stream.CancelRead(quic.StreamErrorCode(0))
		_ = stream.Close()
	}()

	frames := newFrameReader(stream)
	header, err := s.readHeader(stream, frames)
	if err != nil {
		s.log.Warn("session: bad request header", "peer", s.peerID, "error", err)
		s.violate("bad request header")
		return
	}
	if header.GetKind() != wirev1.StreamKind_STREAM_KIND_REQUEST {
		s.violate("transfer header on a request stream")
		return
	}

	route, ok := s.requestRoute(header.GetPayloadType())
	if !ok {
		s.log.Debug("session: dropped a request stream without a handler", "peer", s.peerID, "type", header.GetPayloadType())
		return
	}
	if !s.authorize(route.action, "request "+header.GetPayloadType()) {
		return
	}

	req := &Request{stream: stream, frames: frames, header: header}
	if err := route.handler(s.ctx, req); err != nil {
		s.log.Warn("session: request handler failed", "peer", s.peerID, "type", header.GetPayloadType(), "error", err)
	}
}

// serveTransfer checks an incoming transfer stream and runs its handler.
func (s *Session) serveTransfer(stream *quic.ReceiveStream) {
	defer s.transferLoad.Add(-1)
	defer stream.CancelRead(quic.StreamErrorCode(0))

	frames := newFrameReader(stream)
	header, err := s.readHeader(stream, frames)
	if err != nil {
		s.log.Warn("session: bad transfer header", "peer", s.peerID, "error", err)
		s.violate("bad transfer header")
		return
	}
	if header.GetKind() != wirev1.StreamKind_STREAM_KIND_TRANSFER {
		s.violate("request header on a transfer stream")
		return
	}

	route, ok := s.transferRoute(header.GetPayloadType())
	if !ok {
		s.log.Debug("session: dropped a transfer stream without a handler", "peer", s.peerID, "type", header.GetPayloadType())
		return
	}
	if !s.authorize(route.action, "transfer "+header.GetPayloadType()) {
		return
	}

	transfer := &Transfer{stream: stream, frames: frames, header: header}
	if err := route.handler(s.ctx, transfer); err != nil {
		s.log.Warn("session: transfer handler failed", "peer", s.peerID, "type", header.GetPayloadType(), "error", err)
	}
}

// requestRoute returns the handler of a request payload type.
func (s *Session) requestRoute(payloadType string) (requestRoute, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	route, ok := s.requests[payloadType]
	return route, ok
}

// transferRoute returns the handler of a transfer payload type.
func (s *Session) transferRoute(payloadType string) (transferRoute, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	route, ok := s.transfers[payloadType]
	return route, ok
}

// readHeader reads the first frame of an incoming stream. A peer that opens a
// stream and stays silent loses it after HeaderTimeout.
func (s *Session) readHeader(stream deadlineSetter, frames *frameReader) (*wirev1.StreamHeader, error) {
	if err := stream.SetReadDeadline(s.clock.Now().Add(HeaderTimeout)); err != nil {
		return nil, err
	}
	defer func() { _ = stream.SetReadDeadline(time.Time{}) }()

	header := &wirev1.StreamHeader{}
	if err := frames.readMessage(MaxStreamMessage, header); err != nil {
		return nil, err
	}
	if err := validateStreamHeader(header); err != nil {
		return nil, err
	}
	return header, nil
}

// writeHeader writes the first frame of an outgoing stream.
func (s *Session) writeHeader(ctx context.Context, stream io.Writer, header *wirev1.StreamHeader) error {
	if deadline, ok := ctx.Deadline(); ok {
		setter, ok := stream.(deadlineWriter)
		if ok {
			if err := setter.SetWriteDeadline(deadline); err != nil {
				return err
			}
			defer func() { _ = setter.SetWriteDeadline(time.Time{}) }()
		}
	}
	return writeFrame(stream, header, MaxStreamMessage)
}

// deadlineSetter is implemented by the receive side of a QUIC stream.
type deadlineSetter interface {
	SetReadDeadline(time.Time) error
}

// deadlineWriter is implemented by the send side of a QUIC stream.
type deadlineWriter interface {
	SetWriteDeadline(time.Time) error
}

// streamHeader validates a caller supplied header and sets its kind.
func streamHeader(header *wirev1.StreamHeader, kind wirev1.StreamKind) (*wirev1.StreamHeader, error) {
	if header == nil {
		return nil, fmt.Errorf("%w: nil header", ErrInvalidHeader)
	}
	hdr, ok := proto.Clone(header).(*wirev1.StreamHeader)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected header type", ErrInvalidHeader)
	}
	if got := hdr.GetKind(); got != wirev1.StreamKind_STREAM_KIND_UNSPECIFIED && got != kind {
		return nil, fmt.Errorf("%w: %s on a %s stream", ErrInvalidHeader, got, kind)
	}
	hdr.Kind = kind
	if err := validateStreamHeader(hdr); err != nil {
		return nil, err
	}
	return hdr, nil
}

// validateStreamHeader checks the header fields that come from the network.
func validateStreamHeader(header *wirev1.StreamHeader) error {
	switch header.GetKind() {
	case wirev1.StreamKind_STREAM_KIND_REQUEST, wirev1.StreamKind_STREAM_KIND_TRANSFER:
	default:
		return fmt.Errorf("%w: stream kind %s", ErrInvalidHeader, header.GetKind())
	}
	if err := validatePayloadType(header.GetPayloadType()); err != nil {
		return err
	}
	if id := header.GetTransferId(); len(id) > MaxTransferIDLength {
		return fmt.Errorf("%w: transfer id of %d bytes", ErrInvalidHeader, len(id))
	}
	return nil
}

// validatePayloadType checks the type that selects a stream handler.
func validatePayloadType(payloadType string) error {
	if payloadType == "" || len(payloadType) > MaxPayloadTypeLength || !utf8.ValidString(payloadType) {
		return fmt.Errorf("%w: payload type of %d bytes", ErrInvalidHeader, len(payloadType))
	}
	return nil
}
