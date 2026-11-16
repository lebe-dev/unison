package discovery

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

var (
	wifi     = &net.Interface{Index: 14, Name: "en0", Flags: net.FlagUp | net.FlagMulticast}
	loopback = &net.Interface{Index: 1, Name: "lo0", Flags: net.FlagUp | net.FlagMulticast | net.FlagLoopback}
)

// deniedErr is what a send returns while the system refuses the local network.
func deniedErr() error {
	return &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendmsg", syscall.EHOSTUNREACH)}
}

type lanClock struct{ now time.Time }

func (c *lanClock) Now() time.Time { return c.now }

func newTestAccess() (*lanAccess, *lanClock) {
	clock := &lanClock{now: time.Unix(1_700_000_000, 0)}
	return newLANAccess(clock.Now), clock
}

func TestLANAccessUnknownIsNotBlocked(t *testing.T) {
	access, _ := newTestAccess()
	if access.blocked() {
		t.Fatal("blocked before any send")
	}
}

func TestLANAccessDeniedSendBlocks(t *testing.T) {
	access, _ := newTestAccess()
	access.record(wifi, deniedErr())
	if !access.blocked() {
		t.Fatal("not blocked after a refused send")
	}
}

func TestLANAccessLoopbackIsIgnored(t *testing.T) {
	access, _ := newTestAccess()
	access.record(loopback, deniedErr())
	if access.blocked() {
		t.Fatal("a loopback failure blocks the local network")
	}
	access.record(wifi, deniedErr())
	access.record(loopback, nil)
	if !access.blocked() {
		t.Fatal("a loopback success unblocks the local network")
	}
}

func TestLANAccessOtherErrorsAreIgnored(t *testing.T) {
	access, _ := newTestAccess()
	access.record(wifi, errors.New("network is down"))
	access.record(nil, deniedErr())
	if access.blocked() {
		t.Fatal("blocked by an unrelated failure")
	}
}

func TestLANAccessWrappedDenialBlocks(t *testing.T) {
	access, _ := newTestAccess()
	access.record(wifi, fmt.Errorf("discovery: %w", errors.Join(errors.New("v6"), deniedErr())))
	if !access.blocked() {
		t.Fatal("a wrapped denial is not recognised")
	}
}

func TestLANAccessSuccessUnblocks(t *testing.T) {
	access, _ := newTestAccess()
	access.record(wifi, deniedErr())
	access.record(wifi, nil)
	if access.blocked() {
		t.Fatal("still blocked after a send went through")
	}
}

func TestLANAccessRecentSuccessWins(t *testing.T) {
	access, clock := newTestAccess()
	access.record(wifi, nil)
	clock.now = clock.now.Add(time.Second)
	// Another interface may refuse a send while the local network works.
	access.record(&net.Interface{Index: 20, Name: "bridge100", Flags: net.FlagUp | net.FlagMulticast}, deniedErr())
	if access.blocked() {
		t.Fatal("blocked although a send went through a moment ago")
	}
}

func TestLANAccessDenialExpires(t *testing.T) {
	access, clock := newTestAccess()
	access.record(wifi, deniedErr())
	clock.now = clock.now.Add(lanAccessWindow + time.Second)
	if access.blocked() {
		t.Fatal("an old denial still blocks")
	}
}

func TestLANAccessDenialAfterOldSuccessBlocks(t *testing.T) {
	access, clock := newTestAccess()
	access.record(wifi, nil)
	clock.now = clock.now.Add(lanAccessWindow + time.Second)
	access.record(wifi, deniedErr())
	if !access.blocked() {
		t.Fatal("an old success hides a fresh denial")
	}
}
