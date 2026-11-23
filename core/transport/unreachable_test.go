package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/quic-go/quic-go"
)

func TestIsUnreachable(t *testing.T) {
	tests := map[string]struct {
		err  error
		want bool
	}{
		"nil":               {err: nil, want: false},
		"deadline":          {err: fmt.Errorf("dial: %w", context.DeadlineExceeded), want: true},
		"handshake timeout": {err: &quic.HandshakeTimeoutError{}, want: true},
		"idle timeout":      {err: &quic.IdleTimeoutError{}, want: true},
		"no route":          {err: &net.OpError{Op: "write", Err: os.NewSyscallError("sendto", syscall.EHOSTUNREACH)}, want: true},
		"network down":      {err: &net.OpError{Op: "write", Err: os.NewSyscallError("sendto", syscall.ENETUNREACH)}, want: true},
		"port closed":       {err: &net.OpError{Op: "read", Err: os.NewSyscallError("recvfrom", syscall.ECONNREFUSED)}, want: true},
		"canceled":          {err: context.Canceled, want: false},
		"peer rejected":     {err: fmt.Errorf("verify: %w", ErrPeerRejected), want: false},
		"auth proof":        {err: ErrAuthProof, want: false},
		"other":             {err: errors.New("boom"), want: false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsUnreachable(test.err); got != test.want {
				t.Fatalf("IsUnreachable(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}
