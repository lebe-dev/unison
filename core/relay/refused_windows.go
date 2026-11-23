//go:build windows

package relay

import (
	"errors"

	"golang.org/x/sys/windows"
)

// refused reports a connection that the peer refused: nothing listens on the
// port. Windows reports the refusal of a TCP connection with its own code.
func refused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED)
}
