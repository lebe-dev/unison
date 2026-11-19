//go:build !windows

package relay

import (
	"errors"
	"syscall"
)

// refused reports a connection that the peer refused: nothing listens on the
// port.
func refused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
