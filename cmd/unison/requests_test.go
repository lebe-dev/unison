package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/trust"
)

// friendDevices starts two Friends devices and waits until they see each other.
func friendDevices(t *testing.T, base int) (*testDevice, *testDevice) {
	t.Helper()

	network, bus := newNetwork(t)
	a := newDevice(t, network, bus, base, api.Options{})
	b := newDevice(t, network, bus, base+1000, api.Options{})
	a.start(t)
	b.start(t)
	enableFriends(t, a, b)
	addFriend(t, a, b)
	addFriend(t, b, a)

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if len(a.Share().Peers()) == 1 && len(b.Share().Peers()) == 1 {
			return a, b
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the devices did not connect")
	return nil, nil
}

// addFriend stores the other device as a Friends peer.
func addFriend(t *testing.T, d *testDevice, other *testDevice) {
	t.Helper()

	err := d.AddPeer(context.Background(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     other.name,
		Platform: model.PlatformMacOS,
		Role:     model.RoleFriends,
	})
	if err != nil {
		t.Fatalf("add a friend: %v", err)
	}
}

// waitRequestID waits until the device lists a request with the file and
// returns its id.
func waitRequestID(t *testing.T, d *testDevice, name string) string {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		code, stdout, stderr := runCommand(t, "", "requests", "--endpoint", d.endpoint)
		if code != exitOK {
			t.Fatalf("requests exited with %d, stderr %q", code, stderr)
		}
		if strings.Contains(stdout, name) {
			for _, line := range strings.Split(stdout, "\n") {
				fields := strings.Fields(line)
				if len(fields) > 0 && fields[0] != "ID" {
					return fields[0]
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no request with %q within %v", name, testTimeout)
	return ""
}

// sendTo offers a file to the device.
func sendTo(t *testing.T, from *testDevice, to *testDevice, path string) {
	t.Helper()

	code, _, stderr := runCommand(t, "", "send", path, "--to", to.name, "--endpoint", from.endpoint)
	if code != exitOK {
		t.Fatalf("send exited with %d, stderr %q", code, stderr)
	}
}

func TestRequestsAccept(t *testing.T) {
	a, b := friendDevices(t, 36000)

	data := []byte(strings.Repeat("requested file ", 4096))
	path := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	sendTo(t, a, b, path)

	id := waitRequestID(t, b, "photo.jpg")
	code, stdout, stderr := runCommand(t, "", "requests", "accept", id, "--endpoint", b.endpoint)
	if code != exitOK {
		t.Fatalf("requests accept exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "accepted") || !strings.Contains(stdout, "photo.jpg") {
		t.Fatalf("the output is %q", stdout)
	}
	if saved := waitSaved(t, b, data); filepath.Base(saved) != "photo.jpg" {
		t.Fatalf("the file was saved as %q", saved)
	}

	code, stdout, _ = runCommand(t, "", "requests", "--endpoint", b.endpoint)
	if code != exitOK {
		t.Fatalf("requests exited with %d", code)
	}
	if !strings.Contains(stdout, "no transfer requests") {
		t.Fatalf("the output is %q", stdout)
	}
}

func TestRequestsDecline(t *testing.T) {
	a, b := friendDevices(t, 38000)

	path := filepath.Join(t.TempDir(), "private.bin")
	if err := os.WriteFile(path, []byte(strings.Repeat("declined content ", 4096)), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	sendTo(t, a, b, path)

	id := waitRequestID(t, b, "private.bin")
	code, stdout, stderr := runCommand(t, "", "requests", "decline", id, "--endpoint", b.endpoint)
	if code != exitOK {
		t.Fatalf("requests decline exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "declined") {
		t.Fatalf("the output is %q", stdout)
	}

	entries, err := os.ReadDir(b.downloads)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read the downloads directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a declined request wrote %d files", len(entries))
	}

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		record, err := a.Share().Transfer(id)
		if err == nil && record.State == share.StateDeclined {
			return
		}
		if err == nil && record.State != share.StateOffered && record.State != share.StateDeclined {
			t.Fatalf("the sender transfer is %s", record.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the sender did not learn the decision within %v", testTimeout)
}
