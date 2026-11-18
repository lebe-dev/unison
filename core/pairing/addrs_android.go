//go:build android

package pairing

import (
	"errors"
	"net"
)

// errNoInterfaceList reports that the platform does not list the interfaces.
var errNoInterfaceList = errors.New("the interface list is not available on Android")

// interfaceAddrs fails at once on Android. The listing needs a netlink socket
// that the system refuses to apps, so every attempt only costs a system call
// and an audit record; the app supplies the addresses it needs itself.
func interfaceAddrs() ([]net.Addr, error) {
	return nil, errNoInterfaceList
}
