package main

import (
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/trust"
)

// TestRelaySetAndOff checks that the command stores and clears the relay of the
// group without printing the token.
func TestRelaySetAndOff(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 330, api.Options{})
	device.start(t)

	code, stdout, stderr := runCommand(t, "", "--endpoint", device.endpoint,
		"relay", "set", "relay.example.com:443", "--token", "secret-token")
	if code != exitOK {
		t.Fatalf("relay set exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"relay.example.com:443", "relay token", "stored"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the command does not report %q:\n%s", want, stdout)
		}
	}
	for _, output := range []string{stdout, stderr} {
		if strings.Contains(output, "secret-token") {
			t.Fatalf("the command printed the access token:\n%s", output)
		}
	}

	settings, _, err := device.RelaySettings(t.Context())
	if err != nil {
		t.Fatalf("read the relay settings: %v", err)
	}
	if settings != (trust.RelaySettings{URL: "relay.example.com:443", Token: "secret-token"}) {
		t.Fatalf("stored relay is %+v", settings)
	}

	code, stdout, stderr = runCommand(t, "", "--endpoint", device.endpoint, "relay", "off")
	if code != exitOK {
		t.Fatalf("relay off exited with %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "relay") {
		t.Errorf("the command does not report the relay:\n%s", stdout)
	}
	settings, _, err = device.RelaySettings(t.Context())
	if err != nil {
		t.Fatalf("read the relay settings: %v", err)
	}
	if !settings.Empty() {
		t.Fatalf("the relay is still configured: %+v", settings)
	}
}

// TestRelaySetValidates checks that a bad address or a missing token fails the
// command.
func TestRelaySetValidates(t *testing.T) {
	network, bus := newNetwork(t)
	device := newDevice(t, network, bus, 340, api.Options{})
	device.start(t)

	cases := [][]string{
		{"relay", "set", "ftp://relay.example", "--token", "secret-token"},
		{"relay", "set", "relay.example.com:443"},
	}
	for _, args := range cases {
		code, _, stderr := runCommand(t, "", append([]string{"--endpoint", device.endpoint}, args...)...)
		if code != exitFailure {
			t.Errorf("%v exited with %d, stderr %q", args, code, stderr)
		}
		if strings.Contains(stderr, "secret-token") {
			t.Errorf("%v printed the access token", args)
		}
	}
}
