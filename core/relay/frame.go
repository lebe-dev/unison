package relay

import (
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// Bounds of the frames a client accepts.
const (
	// maxRelayDatagrams bounds the datagram size a relay may advertise.
	maxRelayDatagrams = 4096
	// maxRegisterBytes bounds the registration frame.
	maxRegisterBytes = 8 << 10
	// maxMailboxes bounds the mailboxes of one registration.
	maxMailboxes = 256
)

// Errors of the frame parser.
var (
	errFrameTooLarge  = errors.New("relay: frame too large")
	errMalformedFrame = errors.New("relay: malformed frame")
)

// Transport is the transport of a relay connection.
type Transport uint8

// Relay transports, in the order a client tries them.
const (
	// TransportNone reports no connection.
	TransportNone Transport = iota
	// TransportQUIC is the UDP transport.
	TransportQUIC
	// TransportWebSocket is the TCP fallback.
	TransportWebSocket
)

// String returns the name of the transport.
func (t Transport) String() string {
	switch t {
	case TransportQUIC:
		return "quic"
	case TransportWebSocket:
		return "websocket"
	default:
		return "none"
	}
}

// Limits are the limits the relay applies to a connection.
type Limits struct {
	// MaxDatagramBytes is the payload of one datagram.
	MaxDatagramBytes int
	// MaxMailboxes is the number of mailboxes one connection may join.
	MaxMailboxes int
	// RatePerSecond is the datagram rate of the token.
	RatePerSecond int
	// RateBurst is the burst of the datagram rate.
	RateBurst int
	// Updates reports a relay that changes the mailboxes of a live
	// connection. Without it a changed mailbox set needs a new connection.
	Updates bool
}

// PacketSize returns the largest QUIC packet that fits into one datagram of the
// relay, or zero when the datagrams are too small for QUIC.
func (l Limits) PacketSize() int {
	return PacketSize(l.MaxDatagramBytes)
}

// Status describes the relay connection of a client.
type Status struct {
	// Connected reports a registered connection.
	Connected bool
	// Transport is the transport of the connection.
	Transport Transport
	// Limits are the limits of the connection.
	Limits Limits
}

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

// datagramFrame encodes one datagram of a mailbox.
func datagramFrame(id ID, payload []byte) ([]byte, error) {
	return marshalFrame(&relayv1.Frame{
		Body: &relayv1.Frame_Datagram{Datagram: &relayv1.Datagram{
			MailboxId: id[:],
			Payload:   payload,
		}},
	})
}

// registerFrame encodes the registration of a connection.
func registerFrame(token string, ids []ID) *relayv1.Frame {
	mailboxes := make([][]byte, 0, len(ids))
	for _, id := range ids {
		mailboxes = append(mailboxes, id[:])
	}
	return &relayv1.Frame{
		Body: &relayv1.Frame_Register{Register: &relayv1.Register{
			Token:      []byte(token),
			MailboxIds: mailboxes,
		}},
	}
}

// updateFrame encodes the replacement of the mailbox set of a connection.
func updateFrame(seq uint64, ids []ID) *relayv1.Frame {
	mailboxes := make([][]byte, 0, len(ids))
	for _, id := range ids {
		mailboxes = append(mailboxes, id[:])
	}
	return &relayv1.Frame{
		Body: &relayv1.Frame_Update{Update: &relayv1.Update{
			Seq:        seq,
			MailboxIds: mailboxes,
		}},
	}
}

// writeStreamFrame writes one length prefixed frame to a control stream.
func writeStreamFrame(w io.Writer, data []byte) error {
	var head [10]byte
	n := uvarintPut(head[:], uint64(len(data)))
	if _, err := w.Write(head[:n]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// readStreamFrame reads one length prefixed frame. The length is checked
// against the limit before the payload is allocated.
func readStreamFrame(r io.Reader, limit int) (*relayv1.Frame, error) {
	size, err := readUvarint(r)
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

// uvarintPut writes v and returns the number of bytes used.
func uvarintPut(buf []byte, v uint64) int {
	i := 0
	for v >= 0x80 {
		buf[i] = byte(v) | 0x80
		v >>= 7
		i++
	}
	buf[i] = byte(v)
	return i + 1
}

// readUvarint reads a varint of at most 10 bytes.
func readUvarint(r io.Reader) (uint64, error) {
	var out uint64
	var buf [1]byte
	for i := range 10 {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		out |= uint64(buf[0]&0x7f) << (7 * i)
		if buf[0] < 0x80 {
			return out, nil
		}
	}
	return 0, errMalformedFrame
}

// limitsOf validates the answer to a registration.
func limitsOf(result *relayv1.RegisterResult) (Limits, error) {
	status := result.GetStatus()
	if status != relayv1.RelayStatus_RELAY_STATUS_OK {
		return Limits{}, &rejectedError{status: status}
	}
	wire := result.GetLimits()
	if wire == nil {
		return Limits{}, fmt.Errorf("%w: no limits", errMalformedFrame)
	}
	limits := Limits{
		MaxDatagramBytes: int(wire.GetMaxDatagramBytes()),
		MaxMailboxes:     int(wire.GetMaxMailboxes()),
		RatePerSecond:    int(wire.GetRatePerSecond()),
		RateBurst:        int(wire.GetRateBurst()),
		Updates:          wire.GetMailboxUpdates(),
	}
	switch {
	case limits.MaxDatagramBytes <= 0 || limits.MaxDatagramBytes > maxRelayDatagrams:
		return Limits{}, fmt.Errorf("%w: datagram size %d", errMalformedFrame, limits.MaxDatagramBytes)
	case limits.MaxMailboxes <= 0 || limits.MaxMailboxes > maxMailboxes:
		return Limits{}, fmt.Errorf("%w: mailbox limit %d", errMalformedFrame, limits.MaxMailboxes)
	}
	return limits, nil
}

// mailboxIDOf converts the wire form of a mailbox id.
func mailboxIDOf(raw []byte) (ID, bool) {
	if len(raw) != mailboxIDLen {
		return ID{}, false
	}
	var id ID
	copy(id[:], raw)
	return id, true
}

// rejectedError is a registration the relay refused, with the status it gave.
// It matches ErrRejected.
type rejectedError struct {
	status relayv1.RelayStatus
}

// Error describes the refusal.
func (e *rejectedError) Error() string {
	return ErrRejected.Error() + ": " + statusName(e.status)
}

// Is reports that the error is ErrRejected.
func (e *rejectedError) Is(target error) bool {
	return target == ErrRejected
}

// statusName returns a short name of a relay status for logs and errors.
func statusName(status relayv1.RelayStatus) string {
	switch status {
	case relayv1.RelayStatus_RELAY_STATUS_OK:
		return "ok"
	case relayv1.RelayStatus_RELAY_STATUS_UNAUTHORIZED:
		return "unauthorized"
	case relayv1.RelayStatus_RELAY_STATUS_QUOTA_EXCEEDED:
		return "quota exceeded"
	case relayv1.RelayStatus_RELAY_STATUS_INVALID_ARGUMENT:
		return "invalid argument"
	case relayv1.RelayStatus_RELAY_STATUS_UNAVAILABLE:
		return "unavailable"
	case relayv1.RelayStatus_RELAY_STATUS_DATAGRAMS_REQUIRED:
		return "datagrams required"
	case relayv1.RelayStatus_RELAY_STATUS_SHUTTING_DOWN:
		return "shutting down"
	default:
		return fmt.Sprintf("status %d", int(status))
	}
}
