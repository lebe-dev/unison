//go:build !android

package pairing

import "net"

// interfaceAddrs returns the addresses of the local interfaces.
func interfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}
