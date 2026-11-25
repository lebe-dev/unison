package mediate

import (
	"slices"
	"testing"
)

// TestNormalizeAddrs checks the strict parsing of address hints.
func TestNormalizeAddrs(t *testing.T) {
	tests := []struct {
		name          string
		in            []string
		allowLoopback bool
		want          []string
	}{
		{"public address", []string{"192.0.2.10:4242"}, false, []string{"192.0.2.10:4242"}},
		{"ipv6 address", []string{"[2001:db8::1]:4242"}, false, []string{"[2001:db8::1]:4242"}},
		{"loopback dropped", []string{"127.0.0.1:4242"}, false, nil},
		{"loopback kept in a loopback session", []string{"127.0.0.1:4242"}, true, []string{"127.0.0.1:4242"}},
		{"dns name dropped", []string{"example.com:4242"}, false, nil},
		{"zone dropped", []string{"[fe80::1%eth0]:4242"}, false, nil},
		{"unspecified dropped", []string{"0.0.0.0:4242"}, false, nil},
		{"multicast dropped", []string{"224.0.0.1:4242"}, false, nil},
		{"zero port dropped", []string{"192.0.2.10:0"}, false, nil},
		{"long address dropped", []string{"[2001:0db8:85a3:0000:0000:8a2e:0370:7334:extra:padding:exceeding:64]:4242"}, false, nil},
		{"five addresses dropped", []string{"192.0.2.1:1", "192.0.2.2:2", "192.0.2.3:3", "192.0.2.4:4", "192.0.2.5:5"}, false, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeAddrs(test.in, test.allowLoopback)
			if !slices.Equal(got, test.want) {
				t.Fatalf("addresses %v, want %v", got, test.want)
			}
		})
	}
}

// TestValidateName checks the display names.
func TestValidateName(t *testing.T) {
	valid := []string{"MacBook", "Телефон", "🐶 phone", "a"}
	for _, name := range valid {
		if err := validateName(name); err != nil {
			t.Errorf("name %q refused: %v", name, err)
		}
	}
	invalid := []string{"", "line\nbreak", "right\u202eto\u202c", string([]byte{0xff})}
	for _, name := range invalid {
		if err := validateName(name); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if err := validateName(string(make([]byte, MaxNameLength+1))); err == nil {
		t.Error("an overlong name is accepted")
	}
}
