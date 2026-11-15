// Package session runs the peer protocol on top of QUIC streams.
//
// A session starts on an authenticated connection with the Hello exchange on
// the control stream: both sides announce the protocol version, the device name
// and platform, the local clipboard policy and the optional capabilities. A peer
// with another protocol version is asked to update and the session is closed.
//
// Every message is a length-delimited protobuf: a uvarint payload length
// followed by the message. The length is checked against the size limit before
// the payload is allocated, so a peer cannot make the process allocate more than
// the limits allow.
//
// The control stream is bidirectional and carries ControlMessage envelopes. A
// message is routed to the handler of its kind, but only after the capability
// matrix of the peer allows the action. A message the role of the peer forbids
// is dropped, counted as a protocol violation, and a peer that reaches the
// violation threshold loses the session. Request streams are bidirectional and
// carry one request with its answer, transfer streams are unidirectional and
// carry one transfer. The first frame of either stream is a StreamHeader whose
// payload type selects the registered handler.
package session
