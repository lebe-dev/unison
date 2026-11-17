package main

import "testing"

// TestReportingAllowed checks the values that turn reporting off.
func TestReportingAllowed(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", true},
		{"1", true},
		{"on", true},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{" off ", false},
	} {
		t.Setenv("UNISON_RELAY_SENTRY", tc.value)
		if got := reportingAllowed(); got != tc.want {
			t.Errorf("UNISON_RELAY_SENTRY=%q: got %v, want %v", tc.value, got, tc.want)
		}
	}
}
