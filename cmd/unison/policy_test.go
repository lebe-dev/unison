package main

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/lebe-dev/unison/core/api"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestPolicy checks the human readable policy table.
func TestPolicy(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 120, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "policy")
	if code != exitOK {
		t.Fatalf("policy exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"CONDITION", "unmetered", "metered", "roaming", "data saver", "auto 10.0 MiB", "auto 1.0 MiB", "metadata", "relay limit"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("policy does not report %q:\n%s", want, stdout)
		}
	}
}

// TestPolicyJSON checks the machine readable policy.
func TestPolicyJSON(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 130, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint, "--json", "policy")
	if code != exitOK {
		t.Fatalf("policy exited with %d, stderr %q", code, stderr)
	}

	var response localv1.GetSettingsResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the policy %q: %v", stdout, err)
	}
	settings := response.GetSettings()
	if got := settings.GetUnmetered().GetText().GetAction(); got != localv1.PolicyAction_POLICY_ACTION_AUTO {
		t.Errorf("unmetered text action is %s, want auto", got)
	}
	if got := settings.GetRoaming().GetImage().GetAction(); got != localv1.PolicyAction_POLICY_ACTION_METADATA {
		t.Errorf("roaming image action is %s, want metadata", got)
	}
	if settings.GetRoaming().GetRelay() {
		t.Error("the relay of the roaming row is on")
	}
}
