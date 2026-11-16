package discovery

import (
	"errors"
	"net"
	"sync"
	"syscall"
	"time"
)

// lanAccessWindow is how long one send counts as the evidence of the access to
// the local network. Announcements and queries go out more often than that.
const lanAccessWindow = 2 * time.Minute

// lanAccess follows whether the system lets the process send to the local
// network. macOS refuses the sends of a process without the local network
// permission with EHOSTUNREACH, while the loopback interface keeps working.
type lanAccess struct {
	now func() time.Time

	mu         sync.Mutex
	lastOK     time.Time
	lastDenied time.Time
}

// newLANAccess returns a tracker that reads the time from now.
func newLANAccess(now func() time.Time) *lanAccess {
	return &lanAccess{now: now}
}

// record notes the result of one send on iface. Sends on the loopback
// interface say nothing about the local network and are ignored.
func (a *lanAccess) record(iface *net.Interface, err error) {
	if a == nil || iface == nil || iface.Flags&net.FlagLoopback != 0 {
		return
	}
	if err != nil && !errors.Is(err, syscall.EHOSTUNREACH) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err == nil {
		a.lastOK = a.now()
		return
	}
	a.lastDenied = a.now()
}

// blocked reports a send to the local network that was refused recently while
// none went through. A process the system lets through has the access.
func (a *lanAccess) blocked() bool {
	if a == nil {
		return false
	}
	now := a.now()

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastDenied.IsZero() || now.Sub(a.lastDenied) > lanAccessWindow {
		return false
	}
	return a.lastOK.IsZero() || now.Sub(a.lastOK) > lanAccessWindow
}
