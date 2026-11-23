//go:build linux

package daemon

import (
	"net"
	"path/filepath"
	"testing"
)

// TestProbeDisplay checks that the probe connects to a listening socket and
// reports a missing server.
func TestProbeDisplay(t *testing.T) {
	dir := t.TempDir()
	if err := probeDisplayIn(t.Context(), ":0", dir); err == nil {
		t.Fatal("the probe found a server in an empty directory")
	}

	listener, err := net.Listen("unix", filepath.Join(dir, "X0"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}()

	if err := probeDisplayIn(t.Context(), ":0", dir); err != nil {
		t.Fatalf("probe: %v", err)
	}
	<-accepted
}
