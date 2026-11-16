package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// missingEndpoint returns the endpoint of a daemon that is not there. A command
// line that is accepted reaches the daemon and exits with exitUnreachable, so
// the exit code of the run shows how the arguments were read.
func missingEndpoint(t *testing.T) string {
	t.Helper()

	return filepath.Join(shortDir(t), "missing.sock")
}

// TestCommandFlagsInAnyPosition checks that the flags of a command are accepted
// before, after and between its arguments.
func TestCommandFlagsInAnyPosition(t *testing.T) {
	endpoint := missingEndpoint(t)
	payload := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(payload, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write the payload: %v", err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{
			"devices address after the arguments",
			[]string{"devices", "address", "phone", "127.0.0.1:5000", "--wait", "1s", "--endpoint", endpoint},
		},
		{
			"devices address before the arguments",
			[]string{"--endpoint", endpoint, "devices", "address", "--wait", "1s", "phone", "127.0.0.1:5000"},
		},
		{
			"devices address between the arguments",
			[]string{"devices", "--endpoint", endpoint, "address", "phone", "--wait", "1s", "127.0.0.1:5000"},
		},
		{
			"devices rename after the arguments",
			[]string{"devices", "rename", "phone", "work", "--json", "--endpoint", endpoint},
		},
		{
			"devices rename before the arguments",
			[]string{"--json", "--endpoint", endpoint, "devices", "rename", "phone", "work"},
		},
		{
			"devices rename between the arguments",
			[]string{"devices", "rename", "phone", "--json", "work", "--endpoint", endpoint},
		},
		{
			"devices confirm after the arguments",
			[]string{"devices", "confirm", "phone", "on", "--json", "--endpoint", endpoint},
		},
		{
			"devices confirm before the arguments",
			[]string{"--json", "--endpoint", endpoint, "devices", "confirm", "phone", "on"},
		},
		{
			"devices confirm between the arguments",
			[]string{"devices", "confirm", "phone", "--json", "on", "--endpoint", endpoint},
		},
		{
			"devices revoke after the arguments",
			[]string{"devices", "revoke", "phone", "--yes", "--endpoint", endpoint},
		},
		{
			"devices revoke before the arguments",
			[]string{"--endpoint", endpoint, "devices", "revoke", "--yes", "phone"},
		},
		{
			"devices revoke around the arguments",
			[]string{"devices", "revoke", "--json", "phone", "--yes", "--endpoint", endpoint},
		},
		{
			"history fetch after the arguments",
			[]string{"history", "fetch", "0123", "-o", payload, "--endpoint", endpoint},
		},
		{
			"history fetch before the arguments",
			[]string{"--endpoint", endpoint, "history", "fetch", "-o", payload, "0123"},
		},
		{
			"history fetch around the arguments",
			[]string{"history", "fetch", "--json", "0123", "-o", payload, "--endpoint", endpoint},
		},
		{
			"history apply after the arguments",
			[]string{"history", "apply", "0123", "--json", "--endpoint", endpoint},
		},
		{
			"history apply before the arguments",
			[]string{"--json", "--endpoint", endpoint, "history", "apply", "0123"},
		},
		{
			"history applied around the arguments",
			[]string{"history", "apply", "--json", "0123", "--endpoint", endpoint},
		},
		{
			"requests accept after the arguments",
			[]string{"requests", "accept", "0123", "--json", "--endpoint", endpoint},
		},
		{
			"requests accept before the arguments",
			[]string{"--json", "--endpoint", endpoint, "requests", "accept", "0123"},
		},
		{
			"requests decline after the arguments",
			[]string{"requests", "decline", "0123", "--json", "--endpoint", endpoint},
		},
		{
			"requests decline before the arguments",
			[]string{"--json", "--endpoint", endpoint, "requests", "decline", "0123"},
		},
		{
			"send after the arguments",
			[]string{"send", payload, "--to", "phone", "--endpoint", endpoint},
		},
		{
			"send before the arguments",
			[]string{"--endpoint", endpoint, "send", "--to", "phone", payload},
		},
		{
			"send around the arguments",
			[]string{"send", "--json", payload, "--to", "phone", "--endpoint", endpoint},
		},
		{
			"transfers cancel after the arguments",
			[]string{"transfers", "cancel", "0123", "--json", "--endpoint", endpoint},
		},
		{
			"transfers cancel before the arguments",
			[]string{"--json", "--endpoint", endpoint, "transfers", "cancel", "0123"},
		},
		{
			"friends search after the flags before the subcommand",
			[]string{"--endpoint", endpoint, "friends", "--off", "search", "--json"},
		},
		{
			"friends search before the arguments",
			[]string{"--json", "--endpoint", endpoint, "friends", "search", "--off"},
		},
		{
			"friends search after the arguments",
			[]string{"friends", "search", "--off", "--json", "--endpoint", endpoint},
		},
		{
			"settings ttl after the arguments",
			[]string{"settings", "ttl", "text", "7d", "--json", "--endpoint", endpoint},
		},
		{
			"settings ttl before the arguments",
			[]string{"--json", "--endpoint", endpoint, "settings", "ttl", "text", "7d"},
		},
		{
			"settings ttl around the arguments",
			[]string{"settings", "--json", "ttl", "text", "7d", "--endpoint", endpoint},
		},
		{
			"pause after the argument",
			[]string{"pause", "5m", "--json", "--endpoint", endpoint},
		},
		{
			"pause before the argument",
			[]string{"--json", "--endpoint", endpoint, "pause", "5m"},
		},
		{
			"pause around the argument",
			[]string{"pause", "--json", "5m", "--endpoint", endpoint},
		},
	}
	for _, test := range cases {
		code, _, stderr := runCommand(t, "", test.args...)
		if code != exitUnreachable {
			t.Errorf("%s: %v exited with %d, want %d, stderr %q", test.name, test.args, code, exitUnreachable, stderr)
		}
	}
}

// TestCommandMissingArguments checks that a short command line is refused before
// it reaches the daemon and names the missing argument.
func TestCommandMissingArguments(t *testing.T) {
	endpoint := missingEndpoint(t)

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"devices", "address"}, "needs a device and an address"},
		{[]string{"devices", "address", "phone"}, "needs a device and an address"},
		{[]string{"devices", "address", "--wait", "5s"}, "needs a device and an address"},
		{[]string{"devices", "rename", "phone"}, "needs an id and a name"},
		{[]string{"devices", "confirm", "phone"}, "needs a device and on or off"},
		{[]string{"devices", "confirm", "phone", "on", "off"}, "needs a device and on or off"},
		{[]string{"devices", "revoke"}, "needs one device"},
		{[]string{"devices", "revoke", "--yes"}, "needs one device"},
		{[]string{"history", "fetch", "--json"}, "takes one item id"},
		{[]string{"history", "apply"}, "takes one item id"},
		{[]string{"requests", "accept"}, "takes one request id"},
		{[]string{"requests", "decline", "--json"}, "takes one request id"},
		{[]string{"send", "--to", "phone"}, "takes one path"},
		{[]string{"send", "/tmp/report.txt"}, "needs at least one --to"},
		{[]string{"transfers", "cancel", "--json"}, "takes one transfer id"},
		{[]string{"settings", "ttl", "text"}, "settings ttl takes"},
	}
	for _, test := range cases {
		args := append([]string{"--endpoint", endpoint}, test.args...)
		code, _, stderr := runCommand(t, "", args...)
		if code != exitFailure {
			t.Errorf("%v exited with %d, want %d", test.args, code, exitFailure)
			continue
		}
		if !strings.Contains(stderr, test.want) {
			t.Errorf("%v does not report %q:\n%s", test.args, test.want, stderr)
		}
		if strings.Contains(stderr, "not reachable") {
			t.Errorf("%v reached the daemon before it was complete:\n%s", test.args, stderr)
		}
	}
}

// TestCommandHelpAfterArguments checks that a help flag behind the arguments
// prints the usage instead of an argument error.
func TestCommandHelpAfterArguments(t *testing.T) {
	endpoint := missingEndpoint(t)

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"devices", "address", "phone", "127.0.0.1:5000", "-h"}, "devices address"},
		{[]string{"devices", "address", "phone", "127.0.0.1:5000", "--help"}, "devices address"},
		{[]string{"devices", "rename", "phone", "work", "-h"}, "devices rename"},
		{[]string{"devices", "confirm", "phone", "on", "-h"}, "devices confirm"},
		{[]string{"devices", "revoke", "phone", "--yes", "-h"}, "devices revoke"},
		{[]string{"history", "fetch", "0123", "-o", "out", "-h"}, "history fetch"},
		{[]string{"history", "apply", "0123", "--help"}, "history apply"},
		{[]string{"requests", "accept", "0123", "-h"}, "requests accept"},
		{[]string{"requests", "decline", "0123", "-h"}, "requests decline"},
		{[]string{"send", "report.txt", "--to", "phone", "-h"}, "send"},
		{[]string{"transfers", "cancel", "0123", "-h"}, "transfers cancel"},
		{[]string{"friends", "search", "--off", "-h"}, "friends search"},
		{[]string{"settings", "ttl", "text", "7d", "-h"}, "settings ttl"},
		{[]string{"pause", "5m", "-h"}, "pause"},
	}
	for _, test := range cases {
		args := append([]string{"--endpoint", endpoint}, test.args...)
		code, _, stderr := runCommand(t, "", args...)
		if code != exitOK {
			t.Errorf("%v exited with %d, want %d", test.args, code, exitOK)
			continue
		}
		if !strings.Contains(stderr, "usage: unison") || !strings.Contains(stderr, test.want) {
			t.Errorf("%v does not print the usage of %q:\n%s", test.args, test.want, stderr)
		}
	}
}
