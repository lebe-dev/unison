package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// waitHistory waits until the log of the device holds the wanted number of
// items.
func waitHistory(t *testing.T, device *testDevice, want int) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		device.clock.Advance(clip.DefaultDebounce)
		items, err := device.History().List(context.Background(), history.ListFilter{Limit: history.MaxListLimit})
		if err != nil {
			t.Fatalf("read the history: %v", err)
		}
		if len(items) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the history does not hold %d items", want)
}

// TestHistoryFlags checks the type and limit filters with and without the ls
// subcommand.
func TestHistoryFlags(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 500, api.Options{})
	device.start(t)
	device.emit(t, "the first item")
	device.emit(t, "the second item")
	waitHistory(t, device, 2)

	cases := []struct {
		args []string
		rows int
	}{
		{[]string{"history", "--limit", "1"}, 1},
		{[]string{"history", "--type", "text", "--limit", "1"}, 1},
		{[]string{"history", "--limit", "1", "ls"}, 1},
		{[]string{"history", "ls", "--limit", "1"}, 1},
		{[]string{"history", "--limit", "5"}, 2},
		{[]string{"history", "ls"}, 2},
	}
	for _, test := range cases {
		code, stdout, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, test.args...)...)
		if code != exitOK {
			t.Fatalf("%v exited with %d, stderr %q", test.args, code, stderr)
		}
		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		if len(lines) != test.rows+1 {
			t.Errorf("%v listed %d rows, want %d:\n%s", test.args, len(lines)-1, test.rows, stdout)
		}
	}
}

// TestHistoryTypeFilter checks that the type filter leaves out the other items.
func TestHistoryTypeFilter(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 510, api.Options{})
	device.start(t)
	device.emit(t, "the only text")
	waitHistory(t, device, 1)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "history", "--type", "image")
	if code != exitOK {
		t.Fatalf("history --type image exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "the history is empty") {
		t.Errorf("the filter does not leave out the text item:\n%s", stdout)
	}

	code, _, stderr = runCommand(t, "", "--endpoint", device.endpoint, "history", "--type", "code")
	if code != exitFailure || !strings.Contains(stderr, "unknown type") {
		t.Fatalf("history --type code exited with %d, stderr %q", code, stderr)
	}
}

// TestHistoryJSON checks the machine readable list in both forms.
func TestHistoryJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 520, api.Options{})
	device.start(t)
	device.emit(t, "the first item")
	device.emit(t, "the second item")
	waitHistory(t, device, 2)

	forms := [][]string{
		{"history", "--json", "--limit", "1"},
		{"history", "ls", "--json", "--limit", "1"},
	}
	for _, args := range forms {
		code, stdout, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, args...)...)
		if code != exitOK {
			t.Fatalf("%v exited with %d, stderr %q", args, code, stderr)
		}
		var response localv1.ListHistoryResponse
		if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
			t.Fatalf("%v printed %q: %v", args, stdout, err)
		}
		if len(response.GetItems()) != 1 {
			t.Fatalf("%v listed %d items, want one", args, len(response.GetItems()))
		}
		if got := response.GetItems()[0].GetItem().GetKind(); got != localv1.Kind_KIND_TEXT {
			t.Errorf("%v listed an item of the kind %s", args, got)
		}
		if strings.Contains(stdout, "the first item") || strings.Contains(stdout, "the second item") {
			t.Errorf("%v repeats the payload", args)
		}
	}
}

// TestHistoryHelp checks the usage of the subcommands.
func TestHistoryHelp(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 530, api.Options{})
	device.start(t)

	for _, args := range [][]string{
		{"history", "--help"},
		{"history", "fetch", "--help"},
		{"history", "apply", "--help"},
	} {
		code, _, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, args...)...)
		if code != exitOK {
			t.Fatalf("%v exited with %d, stderr %q", args, code, stderr)
		}
		for _, want := range []string{"history fetch", "history apply", "--limit"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("%v does not print %q:\n%s", args, want, stderr)
			}
		}
	}
}

// TestHistoryUnknown checks a subcommand that does not exist.
func TestHistoryUnknown(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 540, api.Options{})
	device.start(t)

	code, _, stderr := runCommand(t, "", "--endpoint", device.endpoint, "history", "unknown")
	if code != exitFailure {
		t.Fatalf("history unknown exited with %d", code)
	}
	if !strings.Contains(stderr, `unknown history command "unknown"`) {
		t.Errorf("the error does not name the subcommand:\n%s", stderr)
	}
}
