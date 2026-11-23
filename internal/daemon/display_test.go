package daemon

import "testing"

// TestDisplayAddress checks the X server address of a DISPLAY value.
func TestDisplayAddress(t *testing.T) {
	const socketDir = "/tmp/.X11-unix"

	tests := []struct {
		name        string
		display     string
		wantNetwork string
		wantAddress string
	}{
		{name: "local", display: ":0", wantNetwork: "unix", wantAddress: "/tmp/.X11-unix/X0"},
		{name: "local with screen", display: ":1.0", wantNetwork: "unix", wantAddress: "/tmp/.X11-unix/X1"},
		{name: "unix prefix", display: "unix:2", wantNetwork: "unix", wantAddress: "/tmp/.X11-unix/X2"},
		{name: "two digits", display: ":12.1", wantNetwork: "unix", wantAddress: "/tmp/.X11-unix/X12"},
		{name: "host", display: "workstation:0", wantNetwork: "tcp", wantAddress: "workstation:6000"},
		{name: "host with screen", display: "workstation:10.0", wantNetwork: "tcp", wantAddress: "workstation:6010"},
		{name: "localhost", display: "localhost:1", wantNetwork: "tcp", wantAddress: "localhost:6001"},
		{name: "ipv6", display: "[::1]:0", wantNetwork: "tcp", wantAddress: "[::1]:6000"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			network, address, err := displayAddress(test.display, socketDir)
			if err != nil {
				t.Fatalf("displayAddress(%q) = %v", test.display, err)
			}
			if network != test.wantNetwork || address != test.wantAddress {
				t.Fatalf("displayAddress(%q) = %s %s, want %s %s",
					test.display, network, address, test.wantNetwork, test.wantAddress)
			}
		})
	}
}

// TestDisplayAddressInvalid checks the values that do not name a server.
func TestDisplayAddressInvalid(t *testing.T) {
	for _, display := range []string{"", "  ", ":x", ":", "host:", ":-1", "noseparator", "[::1:0"} {
		if _, _, err := displayAddress(display, "/tmp/.X11-unix"); err == nil {
			t.Errorf("displayAddress(%q) has no error", display)
		}
	}
}
