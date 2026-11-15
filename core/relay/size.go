package relay

// Sizes of the relay datagrams.
//
// A relay datagram travels in one QUIC packet of the relay connection, so both
// ends of the relay must fit a frame into it. The relay connection uses the QUIC
// default packet size, which leaves room for a frame of maxFrameBytes bytes.
const (
	// MinPacketSize is the smallest QUIC packet a handshake can use.
	MinPacketSize = 1200
	// outerPacketSize is the QUIC packet size of the relay connection itself.
	outerPacketSize = 1280
	// maxFrameBytes is the largest relay frame one QUIC packet of the relay
	// connection carries: the packet keeps room for the short header, the
	// connection id and the tag, and the DATAGRAM frame adds its own header.
	maxFrameBytes = outerPacketSize - 37 - 3
	// frameOverheadBytes bounds the field headers of a relay frame without its
	// payload.
	frameOverheadBytes = mailboxIDLen + 8
)

// PacketSize returns the largest QUIC packet that fits into one relay datagram
// of maxDatagramBytes bytes. It returns zero when the datagrams cannot carry a
// QUIC packet at all.
func PacketSize(maxDatagramBytes int) int {
	limit := min(maxDatagramBytes, maxFrameBytes)
	for size := limit; size >= MinPacketSize; size-- {
		if size+datagramOverhead(size) <= limit {
			return size
		}
	}
	return 0
}

// datagramOverhead returns the bytes a relay frame adds to a payload of size
// bytes.
func datagramOverhead(size int) int {
	// Datagram message: mailbox id field and payload field.
	body := mailboxIDLen + 2 + 1 + uvarintLen(uint64(size)) + size
	// Frame message: the datagram field with its length.
	return 1 + uvarintLen(uint64(body)) + body - size
}

// uvarintLen returns the length of the varint encoding of v.
func uvarintLen(v uint64) int {
	length := 1
	for v >= 0x80 {
		v >>= 7
		length++
	}
	return length
}
