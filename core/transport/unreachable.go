package transport

import (
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/quic-go/quic-go"
)

// IsUnreachable reports whether a dial failed because the peer did not answer
// or the network has no way to it, as opposed to a peer that answered and
// refused the connection. A device that is turned off and a network that keeps
// its clients apart look the same here.
func IsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var handshakeTimeout *quic.HandshakeTimeoutError
	var idleTimeout *quic.IdleTimeoutError
	if errors.As(err, &handshakeTimeout) || errors.As(err, &idleTimeout) {
		return true
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTDOWN) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
