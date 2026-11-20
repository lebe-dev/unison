//go:build linux

package daemon

import (
	"context"
	"net"
	"time"
)

// x11SocketDir holds the unix sockets of the local X servers.
const x11SocketDir = "/tmp/.X11-unix"

// x11ProbeTimeout bounds the check whether a display accepts connections.
const x11ProbeTimeout = time.Second

// probeDisplay reports whether the X server of the display accepts a
// connection.
func probeDisplay(ctx context.Context, display string) error {
	return probeDisplayIn(ctx, display, x11SocketDir)
}

// probeDisplayIn reports whether the X server of the display accepts a
// connection, with socketDir holding the unix sockets of the local servers.
func probeDisplayIn(ctx context.Context, display, socketDir string) error {
	network, address, err := displayAddress(display, socketDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, x11ProbeTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	return conn.Close()
}
