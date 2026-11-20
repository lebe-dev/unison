package model_test

import (
	"testing"

	"github.com/lebe-dev/unison/core/model"
)

// TestPlatformString checks the names of the platforms.
func TestPlatformString(t *testing.T) {
	cases := map[model.Platform]string{
		model.PlatformUnknown: "unknown",
		model.PlatformMacOS:   "macos",
		model.PlatformWindows: "windows",
		model.PlatformAndroid: "android",
		model.PlatformLinux:   "linux",
	}
	for platform, want := range cases {
		if got := platform.String(); got != want {
			t.Errorf("platform %d: got %q, want %q", platform, got, want)
		}
	}
}
