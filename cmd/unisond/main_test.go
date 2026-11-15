package main

import (
	"log/slog"
	"testing"
)

// TestParseLevel checks the log levels the command accepts.
func TestParseLevel(t *testing.T) {
	for _, test := range []struct {
		name string
		want slog.Level
	}{
		{name: "debug", want: slog.LevelDebug},
		{name: "info", want: slog.LevelInfo},
		{name: "", want: slog.LevelInfo},
		{name: "WARN", want: slog.LevelWarn},
		{name: "error", want: slog.LevelError},
	} {
		got, err := parseLevel(test.name)
		if err != nil {
			t.Errorf("parse level %q: %v", test.name, err)
			continue
		}
		if got != test.want {
			t.Errorf("parse level %q: got %v, want %v", test.name, got, test.want)
		}
	}

	if _, err := parseLevel("verbose"); err == nil {
		t.Error("parse level verbose: got no error")
	}
}

// TestRunUsageErrors checks the exit codes of invalid arguments.
func TestRunUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"-clipboard=files"},
		{"-log-level=verbose"},
		{"-unknown"},
	} {
		if code := run(args); code != exitUsage {
			t.Errorf("run %v: got exit code %d, want %d", args, code, exitUsage)
		}
	}
}

// TestRunVersion checks that the version is printed without starting the
// daemon.
func TestRunVersion(t *testing.T) {
	if code := run([]string{"-version"}); code != exitOK {
		t.Errorf("run -version: got exit code %d, want %d", code, exitOK)
	}
}
