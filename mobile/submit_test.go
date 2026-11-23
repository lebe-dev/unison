//go:build unix

package mobile

import (
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// TestSubmitFileClosesDescriptorWithoutCore checks that the facade takes the
// descriptor over even when it refuses the file before the core sees it.
func TestSubmitFileClosesDescriptorWithoutCore(t *testing.T) {
	_ = Stop()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()

	fd, err := syscall.Dup(int(writer.Fd()))
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	_ = writer.Close()

	meta := marshal(t, &mobilev1.FilePayload{FileName: "a.txt", Size: 1})
	if err := SubmitFile(meta, fd); !errors.Is(err, errNotRunning) {
		t.Fatalf("submit returned %v", err)
	}

	// The reader sees the end of the pipe only once every write end is closed.
	if err := reader.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read returned %v, the descriptor was not closed", err)
	}
}

func TestTransfersGetTheLongTimeout(t *testing.T) {
	for _, method := range []string{"FetchHistoryItem", "/unison.local.v1.LocalService/FetchHistoryItem"} {
		if got := callTimeoutOf(method); got != transferTimeout {
			t.Errorf("timeout of %s is %v", method, got)
		}
	}
	if got := callTimeoutOf("GetStatus"); got != callTimeout {
		t.Errorf("timeout of GetStatus is %v", got)
	}
}
