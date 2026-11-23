//go:build linux

package keystore

import "github.com/godbus/dbus/v5"

// openSessionBus returns the session bus of the desktop session. A session
// without a bus is not started: the daemon then has no Secret Service either.
func openSessionBus() (busConn, error) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return sessionConn{conn}, nil
}

// sessionConn adapts the session bus connection to busConn.
type sessionConn struct{ *dbus.Conn }

func (c sessionConn) Object(dest string, path dbus.ObjectPath) busObject {
	return c.Conn.Object(dest, path)
}
