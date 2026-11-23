package clipboard

import "errors"

var (
	// ErrNoWaylandDisplay reports that the session has no Wayland compositor
	// to talk to.
	ErrNoWaylandDisplay = errors.New("clipboard: no Wayland compositor")
	// ErrNoDataControl reports a compositor without a data-control protocol,
	// which keeps the selection out of reach of the daemon. GNOME offers no
	// such protocol.
	ErrNoDataControl = errors.New("clipboard: the compositor offers no data-control protocol")
)
