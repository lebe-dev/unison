package pairing

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"
)

// maxMessageSize bounds one pairing message in bytes.
const maxMessageSize = 64 << 10

// frameBufferSize is the read buffer of a framed stream.
const frameBufferSize = 4096

// Errors reported by the framing layer.
var (
	// ErrFrame reports a malformed, truncated or oversized frame.
	ErrFrame = errors.New("pairing: bad frame")
	// ErrMessage reports a payload that is not a valid protobuf message.
	ErrMessage = errors.New("pairing: bad message")
)

// frameReader reads length-delimited frames. A frame is a uvarint payload
// length followed by a protobuf message. A frame of zero bytes carries an empty
// message.
type frameReader struct {
	buf *bufio.Reader
}

// newFrameReader reads frames from src.
func newFrameReader(src io.Reader) *frameReader {
	return &frameReader{buf: bufio.NewReaderSize(src, frameBufferSize)}
}

// readMessage decodes the next frame into msg.
func (r *frameReader) readMessage(msg proto.Message) error {
	size, err := binary.ReadUvarint(r.buf)
	if err != nil {
		switch {
		case errors.Is(err, io.EOF):
			return io.EOF
		case errors.Is(err, io.ErrUnexpectedEOF):
			return fmt.Errorf("%w: length prefix: %w", ErrFrame, err)
		default:
			return fmt.Errorf("pairing: read frame: %w", err)
		}
	}
	if size > maxMessageSize {
		return fmt.Errorf("%w: payload of %d bytes, limit %d", ErrFrame, size, maxMessageSize)
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(r.buf, payload); err != nil {
		return fmt.Errorf("%w: truncated payload: %w", ErrFrame, err)
	}
	if err := proto.Unmarshal(payload, msg); err != nil {
		return fmt.Errorf("%w: %w", ErrMessage, err)
	}
	return nil
}

// writeMessage encodes msg as one frame and writes it to w.
func writeMessage(w io.Writer, msg proto.Message) error {
	size := proto.Size(msg)
	if size > maxMessageSize {
		return fmt.Errorf("%w: payload of %d bytes, limit %d", ErrFrame, size, maxMessageSize)
	}

	frame := make([]byte, 0, binary.MaxVarintLen64+size)
	frame = binary.AppendUvarint(frame, uint64(size))
	frame, err := proto.MarshalOptions{}.MarshalAppend(frame, msg)
	if err != nil {
		return fmt.Errorf("pairing: marshal: %w", err)
	}
	if _, err := w.Write(frame); err != nil {
		return err
	}
	return nil
}
