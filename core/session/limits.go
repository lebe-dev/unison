package session

import "time"

// ProtocolVersion is the peer protocol version announced in Hello. A peer that
// announces another version is asked to update.
const ProtocolVersion uint32 = 1

// Timing and resource limits. Every session applies them to its peer.
const (
	// HelloTimeout bounds the Hello exchange that opens a session.
	HelloTimeout = 10 * time.Second
	// HeaderTimeout bounds the first frame of an incoming stream.
	HeaderTimeout = 10 * time.Second

	// MaxControlMessage is the largest control message payload in bytes.
	MaxControlMessage = 1 << 20
	// MaxStreamMessage is the largest frame payload on a request or transfer
	// stream in bytes. Stream frames carry payload chunks, so they are larger
	// than control messages.
	MaxStreamMessage = 2 << 20

	// MaxRequestStreams bounds the concurrently served incoming request streams.
	MaxRequestStreams = 16
	// MaxTransferStreams bounds the concurrently served incoming transfer streams.
	MaxTransferStreams = 32

	// MaxCapabilities bounds the capabilities announced in Hello.
	MaxCapabilities = 32
	// MaxCapabilityLength bounds one capability name in bytes.
	MaxCapabilityLength = 64
	// MaxNameLength bounds the device name announced in Hello in bytes.
	MaxNameLength = 128
	// MaxPayloadTypeLength bounds the payload type of a stream in bytes.
	MaxPayloadTypeLength = 64
	// MaxTransferIDLength bounds the transfer ID of a stream in bytes.
	MaxTransferIDLength = 128

	// MaxViolations is the number of protocol violations a peer may commit
	// before the session is closed.
	MaxViolations = 3

	// ClipAnnounceRate is the sustained number of clip announcements accepted
	// per second.
	ClipAnnounceRate = 5
	// ClipAnnounceBurst is the number of clip announcements a peer may send at
	// once before ClipAnnounceRate applies.
	ClipAnnounceBurst = 10

	// EventBuffer is the number of session events buffered for the reader. A
	// reader that falls behind misses events instead of stalling the session.
	EventBuffer = 16
)
