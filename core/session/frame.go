package session

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"
)

// frameBufferSize is the read buffer of a framed stream.
const frameBufferSize = 4096

// Errors reported by the framing layer.
var (
	// ErrFrame reports a malformed, truncated or oversized frame.
	ErrFrame = errors.New("session: bad frame")
	// ErrMessage reports a payload that is not a valid protobuf message.
	ErrMessage = errors.New("session: bad message")
	// ErrMessageTooLarge reports an outgoing message over the size limit.
	ErrMessageTooLarge = errors.New("session: message too large")
)

// frameReader reads length-delimited frames. A frame is a uvarint payload
// length followed by a protobuf message.
type frameReader struct {
	buf *bufio.Reader
}

// newFrameReader reads frames from src.
func newFrameReader(src io.Reader) *frameReader {
	return &frameReader{buf: bufio.NewReaderSize(src, frameBufferSize)}
}

// readFrame returns the payload of the next frame. The length prefix is checked
// against limit before the payload is allocated.
func (r *frameReader) readFrame(limit int) ([]byte, error) {
	size, err := binary.ReadUvarint(r.buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("%w: length prefix: %w", ErrFrame, err)
	}
	if size == 0 || size > uint64(limit) {
		return nil, fmt.Errorf("%w: payload of %d bytes, limit %d", ErrFrame, size, limit)
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(r.buf, payload); err != nil {
		return nil, fmt.Errorf("%w: truncated payload: %w", ErrFrame, err)
	}
	return payload, nil
}

// readMessage decodes the next frame into msg.
func (r *frameReader) readMessage(limit int, msg proto.Message) error {
	payload, err := r.readFrame(limit)
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(payload, msg); err != nil {
		return fmt.Errorf("%w: %w", ErrMessage, err)
	}
	return nil
}

// writeFrame encodes msg as one frame and writes it to w. The message is sized
// before it is marshalled, so an oversized message is never built.
func writeFrame(w io.Writer, msg proto.Message, limit int) error {
	size := proto.Size(msg)
	if size == 0 || size > limit {
		return fmt.Errorf("%w: payload of %d bytes, limit %d", ErrMessageTooLarge, size, limit)
	}

	payload := make([]byte, 0, binary.MaxVarintLen64+size)
	payload = binary.AppendUvarint(payload, uint64(size))
	payload, err := proto.MarshalOptions{}.MarshalAppend(payload, msg)
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}
