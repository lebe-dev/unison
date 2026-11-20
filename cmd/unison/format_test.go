package main

import (
	"testing"
	"time"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

func TestDeviceState(t *testing.T) {
	tests := map[string]struct {
		device *localv1.Device
		want   string
	}{
		"offline":     {device: &localv1.Device{}, want: "offline"},
		"unreachable": {device: &localv1.Device{Unreachable: true}, want: "unreachable"},
		"online":      {device: &localv1.Device{Online: true, Unreachable: true}, want: "online"},
		"revoked":     {device: &localv1.Device{Revoked: true, Unreachable: true}, want: "revoked"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := deviceState(test.device); got != test.want {
				t.Fatalf("deviceState = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPauseStateText(t *testing.T) {
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := map[string]struct {
		pause *localv1.Pause
		want  string
	}{
		"off":   {pause: &localv1.Pause{}, want: "off"},
		"on":    {pause: &localv1.Pause{Paused: true, Indefinite: true}, want: "on"},
		"until": {pause: &localv1.Pause{Paused: true, UntilUnixMs: until.UnixMilli()}, want: "until " + formatTime(until.UnixMilli())},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := pauseStateText(test.pause); got != test.want {
				t.Fatalf("pauseStateText = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPlatformText(t *testing.T) {
	tests := map[string]struct {
		platform localv1.Platform
		want     string
	}{
		"macos":   {platform: localv1.Platform_PLATFORM_MACOS, want: "macos"},
		"windows": {platform: localv1.Platform_PLATFORM_WINDOWS, want: "windows"},
		"android": {platform: localv1.Platform_PLATFORM_ANDROID, want: "android"},
		"linux":   {platform: localv1.Platform_PLATFORM_LINUX, want: "linux"},
		"unknown": {platform: localv1.Platform_PLATFORM_UNSPECIFIED, want: "unknown"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := platformText(test.platform); got != test.want {
				t.Fatalf("platformText = %q, want %q", got, test.want)
			}
		})
	}
}
