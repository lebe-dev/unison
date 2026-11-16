package pairing

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// testQR returns a code with fixed fields.
func testQR() QR {
	return QR{
		DeviceID:    model.DeviceID{0x01, 0x02, 0x03},
		Fingerprint: identity.CertFingerprint{0x04, 0x05},
		Token:       Token{0x06, 0x07},
		Addrs:       []string{"192.168.1.5:41234", "[fd00::1]:41234"},
		Role:        model.RolePersonal,
	}
}

// TestQRRoundTrip checks that a code is written and read back unchanged.
func TestQRRoundTrip(t *testing.T) {
	code := testQR()
	uri, err := code.URI()
	if err != nil {
		t.Fatalf("build the code: %v", err)
	}
	if !strings.HasPrefix(uri, "unison://pair?v=1&id=") {
		t.Errorf("code %q does not start with the version and the device id", uri)
	}
	if !strings.Contains(uri, "&a=192.168.1.5:41234,[fd00::1]:41234&") {
		t.Errorf("code %q does not carry the addresses", uri)
	}
	if !strings.HasSuffix(uri, "&role=personal") {
		t.Errorf("code %q does not end with the role", uri)
	}

	parsed, err := ParseQR(uri)
	if err != nil {
		t.Fatalf("parse the code: %v", err)
	}
	if parsed.DeviceID != code.DeviceID {
		t.Errorf("device id = %s, want %s", parsed.DeviceID, code.DeviceID)
	}
	if parsed.Fingerprint != code.Fingerprint {
		t.Errorf("fingerprint = %s, want %s", parsed.Fingerprint, code.Fingerprint)
	}
	if parsed.Token != code.Token {
		t.Errorf("token = %s, want %s", parsed.Token, code.Token)
	}
	if !slices.Equal(parsed.Addrs, code.Addrs) {
		t.Errorf("addresses = %v, want %v", parsed.Addrs, code.Addrs)
	}
	if parsed.Role != code.Role {
		t.Errorf("role = %s, want %s", parsed.Role, code.Role)
	}
}

// TestQRFriendsRole checks a code that proposes the Friends role.
func TestQRFriendsRole(t *testing.T) {
	code := testQR()
	code.Role = model.RoleFriends
	uri, err := code.URI()
	if err != nil {
		t.Fatalf("build the code: %v", err)
	}
	parsed, err := ParseQR(uri)
	if err != nil {
		t.Fatalf("parse the code: %v", err)
	}
	if parsed.Role != model.RoleFriends {
		t.Fatalf("role = %s, want friends", parsed.Role)
	}
}

// TestQRNormalizesAddrs checks that addresses are written in canonical form.
func TestQRNormalizesAddrs(t *testing.T) {
	code := testQR()
	code.Addrs = []string{"192.168.001.5:41234"}
	if _, err := code.URI(); !errors.Is(err, ErrQR) {
		t.Fatalf("code with a non canonical address = %v, want %v", err, ErrQR)
	}

	code.Addrs = []string{"192.168.1.5:41234", "192.168.1.5:41234"}
	uri, err := code.URI()
	if err != nil {
		t.Fatalf("build the code: %v", err)
	}
	parsed, err := ParseQR(uri)
	if err != nil {
		t.Fatalf("parse the code: %v", err)
	}
	if want := []string{"192.168.1.5:41234"}; !slices.Equal(parsed.Addrs, want) {
		t.Fatalf("addresses = %v, want %v", parsed.Addrs, want)
	}
}

// TestQRNormalizesIPv6 checks that an IPv6 address is written with its
// canonical text form.
func TestQRNormalizesIPv6(t *testing.T) {
	code := testQR()
	code.Addrs = []string{"[FD00:0:0:0:0:0:0:1]:41234"}
	uri, err := code.URI()
	if err != nil {
		t.Fatalf("build the code: %v", err)
	}
	if !strings.Contains(uri, "&a=[fd00::1]:41234&") {
		t.Fatalf("code %q does not carry the canonical address", uri)
	}

	parsed, err := ParseQR(uri)
	if err != nil {
		t.Fatalf("parse the code: %v", err)
	}
	if want := []string{"[fd00::1]:41234"}; !slices.Equal(parsed.Addrs, want) {
		t.Fatalf("addresses = %v, want %v", parsed.Addrs, want)
	}
}

// TestQRBuildChecks checks the fields a code cannot be built without.
func TestQRBuildChecks(t *testing.T) {
	tests := map[string]func(*QR){
		"no device id":   func(q *QR) { q.DeviceID = model.DeviceID{} },
		"no fingerprint": func(q *QR) { q.Fingerprint = identity.CertFingerprint{} },
		"no token":       func(q *QR) { q.Token = Token{} },
		"no address":     func(q *QR) { q.Addrs = nil },
		"no role":        func(q *QR) { q.Role = model.RoleUnspecified },
		"too many addresses": func(q *QR) {
			q.Addrs = []string{"a:1", "b:2", "c:3", "d:4", "e:5"}
		},
		"bad address": func(q *QR) { q.Addrs = []string{"localhost:41234"} },
		"long address": func(q *QR) {
			q.Addrs = []string{strings.Repeat("1", maxAddressLength) + ":1"}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			code := testQR()
			mutate(&code)
			if _, err := code.URI(); !errors.Is(err, ErrQR) {
				t.Fatalf("build = %v, want %v", err, ErrQR)
			}
		})
	}
}

// TestParseQRRejects checks the strictness of the parser.
func TestParseQRRejects(t *testing.T) {
	valid := testURI(t, testQR())
	field := func(name string) string {
		for _, part := range strings.Split(strings.TrimPrefix(valid, qrPrefix), "&") {
			if value, ok := strings.CutPrefix(part, name+"="); ok {
				return value
			}
		}
		t.Fatalf("the code has no %s parameter", name)
		return ""
	}
	replace := func(name, value string) string {
		return strings.Replace(valid, name+"="+field(name), name+"="+value, 1)
	}

	tests := map[string]string{
		"empty":            "",
		"another scheme":   strings.Replace(valid, "unison", "https", 1),
		"another host":     strings.Replace(valid, "pair?", "pairing?", 1),
		"no parameters":    qrPrefix,
		"other version":    replace(qrParamVersion, "2"),
		"no version":       strings.Replace(valid, "v=1&", "", 1),
		"unknown":          valid + "&x=1",
		"duplicate":        valid + "&role=friends",
		"empty value":      replace(qrParamRole, ""),
		"bad role":         replace(qrParamRole, "family"),
		"upper role":       replace(qrParamRole, "Personal"),
		"bad device id":    replace(qrParamDeviceID, strings.Repeat("zz", 32)),
		"short device id":  replace(qrParamDeviceID, strings.Repeat("00", 31)),
		"upper device id":  replace(qrParamDeviceID, strings.Repeat("AA", 32)),
		"bad fingerprint":  replace(qrParamFingerprint, strings.Repeat("00", 31)),
		"short token":      replace(qrParamToken, strings.Repeat("00", 15)),
		"upper token":      replace(qrParamToken, strings.Repeat("AA", 16)),
		"no addresses":     replace(qrParamAddrs, ""),
		"no port":          replace(qrParamAddrs, "192.168.1.5"),
		"port zero":        replace(qrParamAddrs, "192.168.1.5:0"),
		"wildcard":         replace(qrParamAddrs, "0.0.0.0:41234"),
		"hostname":         replace(qrParamAddrs, "example.com:41234"),
		"zone":             replace(qrParamAddrs, "[fe80::1%25en0]:41234"),
		"space":            replace(qrParamAddrs, "192.168.1.5:41234, 10.0.0.1:41234"),
		"too many":         replace(qrParamAddrs, "1.1.1.1:1,2.2.2.2:2,3.3.3.3:3,4.4.4.4:4,5.5.5.5:5"),
		"too long":         valid + strings.Repeat("0", maxQRLength),
		"escaped address":  replace(qrParamAddrs, "192.168.1.5%3A41234"),
		"trailing &:":      valid + "&",
		"fragment":         valid + "#x",
		"another path":     strings.Replace(valid, "unison://pair?", "unison://pair/x?", 1),
		"uppercase scheme": strings.Replace(valid, "unison:", "Unison:", 1),
	}
	for name, uri := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseQR(uri); !errors.Is(err, ErrQR) {
				t.Fatalf("parse = %v, want %v", err, ErrQR)
			}
		})
	}
}

// TestLocalAddresses checks the interface addresses of a code.
func TestLocalAddresses(t *testing.T) {
	addrs, err := localAddresses(41234)
	if err != nil {
		t.Skipf("the host has no usable address: %v", err)
	}
	if len(addrs) == 0 || len(addrs) > maxQRAddresses {
		t.Fatalf("addresses = %v", addrs)
	}
	for _, addr := range addrs {
		parsed, err := parseAddress(addr)
		if err != nil {
			t.Fatalf("address %q: %v", addr, err)
		}
		if parsed != addr {
			t.Errorf("address %q is not canonical, want %q", addr, parsed)
		}
		if !strings.HasSuffix(addr, ":41234") {
			t.Errorf("address %q has another port", addr)
		}
	}

	if _, err := localAddresses(0); err == nil {
		t.Error("port zero is accepted")
	}
}

// testURI returns the text of a code.
func testURI(t *testing.T, code QR) string {
	t.Helper()
	uri, err := code.URI()
	if err != nil {
		t.Fatalf("build the code: %v", err)
	}
	return uri
}

// FuzzParseQR checks that any text either is refused or describes a code that
// can be written and read again.
func FuzzParseQR(f *testing.F) {
	seed := testQR()
	uri, err := seed.URI()
	if err != nil {
		f.Fatalf("build the code: %v", err)
	}
	f.Add(uri)
	f.Add("")
	f.Add("unison://pair?")
	f.Add("unison://pair?v=1")
	f.Add(strings.Replace(uri, "role=personal", "role=friends", 1))
	f.Add(uri + "&a=127.0.0.1:1")

	f.Fuzz(func(t *testing.T, text string) {
		code, err := ParseQR(text)
		if err != nil {
			return
		}
		if len(text) > maxQRLength {
			t.Fatalf("a code of %d bytes was accepted", len(text))
		}
		if len(code.Addrs) == 0 || len(code.Addrs) > maxQRAddresses {
			t.Fatalf("a code with %d addresses was accepted", len(code.Addrs))
		}
		if code.DeviceID.IsZero() || code.Fingerprint.IsZero() || code.Token.IsZero() {
			t.Fatalf("a code without identity fields was accepted: %+v", code)
		}
		if code.Role != model.RolePersonal && code.Role != model.RoleFriends {
			t.Fatalf("a code with role %s was accepted", code.Role)
		}

		written, err := code.URI()
		if err != nil {
			t.Fatalf("a parsed code cannot be written again: %v", err)
		}
		again, err := ParseQR(written)
		if err != nil {
			t.Fatalf("a written code cannot be parsed: %v", err)
		}
		if again.DeviceID != code.DeviceID || again.Fingerprint != code.Fingerprint ||
			again.Token != code.Token || again.Role != code.Role ||
			!slices.Equal(again.Addrs, code.Addrs) {
			t.Fatalf("the code changed: %+v and %+v", code, again)
		}
	})
}
