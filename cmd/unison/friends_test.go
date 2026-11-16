package main

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/lebe-dev/unison/core/api"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestFriendsSearch checks that the command turns the search on, extends it and
// turns it off again.
func TestFriendsSearch(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 300, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "friends", "search")
	if code != exitOK {
		t.Fatalf("friends search exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"friends discovery: on until", "other device"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the command does not report %q:\n%s", want, stdout)
		}
	}
	if on, _, err := device.FriendsDiscovery(t.Context()); err != nil || !on {
		t.Fatalf("the search is off after the command (%v)", err)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "friends", "search", "--off")
	if code != exitOK {
		t.Fatalf("friends search --off exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "friends discovery: off") {
		t.Errorf("the command does not report the end of the search:\n%s", stdout)
	}
	if on, _, err := device.FriendsDiscovery(t.Context()); err != nil || on {
		t.Fatalf("the search is on after it was turned off (%v)", err)
	}
}

// TestFriendsSearchJSON checks the machine readable answer of the command.
func TestFriendsSearchJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 310, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "--json", "friends", "search")
	if code != exitOK {
		t.Fatalf("friends search exited with %d, stderr %q", code, stderr)
	}

	var response localv1.SetFriendsDiscoveryResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the answer %q: %v", stdout, err)
	}
	if !response.GetFriendsDiscovery().GetOn() {
		t.Fatal("the answer reports the search off")
	}
	if response.GetFriendsDiscovery().GetUntilUnixMs() == 0 {
		t.Fatal("the answer has no end of the search")
	}
}

// TestStatusReportsFriendsSearch checks that the status shows the running
// search.
func TestStatusReportsFriendsSearch(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 320, api.Options{})
	device.start(t)

	if code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "friends", "search"); code != exitOK {
		t.Fatalf("friends search exited with %d, stderr %q", code, stderr)
	}
	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "status")
	if code != exitOK {
		t.Fatalf("status exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "searching until") {
		t.Errorf("the status does not report the search:\n%s", stdout)
	}
}
