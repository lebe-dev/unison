package xdg_test

import (
	"testing"

	"github.com/lebe-dev/unison/internal/xdg"
)

// TestDataHome checks that only an absolute override replaces the default.
func TestDataHome(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		want string
	}{
		{name: "absolute", dir: "/data/home", want: "/data/home"},
		{name: "trailing slash", dir: "/data/home/", want: "/data/home"},
		{name: "empty", dir: "", want: "/home/user/.local/share"},
		{name: "relative", dir: "data", want: "/home/user/.local/share"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := xdg.DataHome("/home/user", test.dir); got != test.want {
				t.Fatalf("data home is %q, want %q", got, test.want)
			}
		})
	}
}

// TestConfigHome checks that only an absolute override replaces the default.
func TestConfigHome(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		want string
	}{
		{name: "absolute", dir: "/data/config", want: "/data/config"},
		{name: "trailing slash", dir: "/data/config/", want: "/data/config"},
		{name: "empty", dir: "", want: "/home/user/.config"},
		{name: "relative", dir: "config", want: "/home/user/.config"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := xdg.ConfigHome("/home/user", test.dir); got != test.want {
				t.Fatalf("config home is %q, want %q", got, test.want)
			}
		})
	}
}
