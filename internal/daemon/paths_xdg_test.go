package daemon

import "testing"

// TestLinuxDataDir checks that the XDG data home wins when it is absolute and
// that a relative value falls back to the home directory.
func TestLinuxDataDir(t *testing.T) {
	cases := []struct {
		name string
		xdg  string
		want string
	}{
		{name: "xdg", xdg: "/data", want: "/data/unison"},
		{name: "empty", xdg: "", want: "/home/user/.local/share/unison"},
		{name: "relative", xdg: "data", want: "/home/user/.local/share/unison"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxDataDir("/home/user", test.xdg); got != test.want {
				t.Fatalf("data dir is %q, want %q", got, test.want)
			}
		})
	}
}
