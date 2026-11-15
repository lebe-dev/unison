package relay

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

const (
	// mailboxIDLen is the size of a mailbox id.
	mailboxIDLen = 16
	// maxRegisterBytes bounds the registration frame of a connection.
	maxRegisterBytes = 8 << 10
	// frameOverhead bounds the message framing around a datagram payload.
	frameOverhead = 64
	// streamBufferSize is the read buffer of a control stream.
	streamBufferSize = 512
)

// Errors of the frame parser.
var (
	errFrameTooLarge  = errors.New("relay: frame too large")
	errMalformedFrame = errors.New("relay: malformed frame")
)

// marshalFrame encodes a frame.
func marshalFrame(frame *relayv1.Frame) ([]byte, error) {
	data, err := proto.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("relay: encode frame: %w", err)
	}
	return data, nil
}

// parseFrame decodes a frame of at most limit bytes.
func parseFrame(data []byte, limit int) (*relayv1.Frame, error) {
	if len(data) == 0 {
		return nil, errMalformedFrame
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%w: %d bytes", errFrameTooLarge, len(data))
	}
	frame := new(relayv1.Frame)
	if err := proto.Unmarshal(data, frame); err != nil {
		return nil, fmt.Errorf("%w: %w", errMalformedFrame, err)
	}
	return frame, nil
}

// readStreamFrame reads one length prefixed frame from a control stream. The
// length is checked against the limit before the payload is allocated.
func readStreamFrame(r *bufio.Reader, limit int) (*relayv1.Frame, error) {
	size, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, err
	}
	if size > uint64(limit) {
		return nil, fmt.Errorf("%w: %d bytes", errFrameTooLarge, size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return parseFrame(data, limit)
}

// writeStreamFrame writes one length prefixed frame to a control stream.
func writeStreamFrame(w io.Writer, data []byte) error {
	var head [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(head[:], uint64(len(data)))
	if _, err := w.Write(head[:n]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// closeFrame encodes the terminal notification of a connection.
func closeFrame(status relayv1.RelayStatus) []byte {
	data, err := marshalFrame(&relayv1.Frame{
		Body: &relayv1.Frame_Close{Close: &relayv1.Close{Status: status}},
	})
	if err != nil {
		return nil
	}
	return data
}

// registerResultFrame encodes the answer to a registration.
func registerResultFrame(status relayv1.RelayStatus, limits *relayv1.Limits) []byte {
	data, err := marshalFrame(&relayv1.Frame{
		Body: &relayv1.Frame_RegisterResult{
			RegisterResult: &relayv1.RegisterResult{Status: status, Limits: limits},
		},
	})
	if err != nil {
		return nil
	}
	return data
}

// mailboxIDOf converts the wire form of a mailbox id.
func mailboxIDOf(raw []byte) (mailboxID, bool) {
	if len(raw) != mailboxIDLen {
		return mailboxID{}, false
	}
	var id mailboxID
	copy(id[:], raw)
	return id, true
}
