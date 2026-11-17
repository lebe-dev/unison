package main

import (
	"testing"

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
