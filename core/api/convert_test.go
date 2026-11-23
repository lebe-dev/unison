package api

import (
	"testing"

	"github.com/lebe-dev/unison/core/model"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestPlatformToProto checks that every platform has a local API value.
func TestPlatformToProto(t *testing.T) {
	cases := map[model.Platform]localv1.Platform{
		model.PlatformUnknown: localv1.Platform_PLATFORM_UNSPECIFIED,
		model.PlatformMacOS:   localv1.Platform_PLATFORM_MACOS,
		model.PlatformWindows: localv1.Platform_PLATFORM_WINDOWS,
		model.PlatformAndroid: localv1.Platform_PLATFORM_ANDROID,
		model.PlatformLinux:   localv1.Platform_PLATFORM_LINUX,
	}
	for platform, want := range cases {
		if got := platformToProto(platform); got != want {
			t.Errorf("platform %s: got %s, want %s", platform, got, want)
		}
	}
}
