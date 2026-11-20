//go:build !linux

package keystore

// openSessionBus reports that this platform has no Secret Service.
func openSessionBus() (busConn, error) {
	return nil, ErrUnsupported
}
