package mediate

import (
	"testing"

	"github.com/lebe-dev/unison/core/identity"
)

// FuzzNormalizeAddrs checks that an arbitrary address hint cannot panic the
// parser.
func FuzzNormalizeAddrs(f *testing.F) {
	f.Add("192.0.2.10:4242")
	f.Add("[2001:db8::1]:4242")
	f.Add("127.0.0.1:1")
	f.Add("")
	f.Add("example.com:4242")
	f.Fuzz(func(_ *testing.T, addr string) {
		normalizeAddrs([]string{addr}, true)
		normalizeAddrs([]string{addr, addr, addr, addr, addr}, false)
	})
}

// FuzzSanitizeName checks that an arbitrary name cannot panic the validator.
func FuzzSanitizeName(f *testing.F) {
	f.Add("MacBook")
	f.Add("Телефон")
	f.Add("\u202e")
	f.Add("")
	f.Fuzz(func(_ *testing.T, name string) {
		_ = sanitizeName(name)
	})
}

// FuzzOfferKeys checks that an arbitrary key pair from an offer is parsed
// without a panic and that a parsed identity is complete.
func FuzzOfferKeys(f *testing.F) {
	valid := make([]byte, 1952)
	for i := range valid {
		valid[i] = byte(i)
	}
	f.Add(make([]byte, 32), valid)
	f.Add([]byte("short"), []byte("short"))
	f.Fuzz(func(t *testing.T, ed, pq []byte) {
		public, err := identity.NewPublicIdentity(ed, pq)
		if err != nil {
			return
		}
		if len(public.Ed25519) != 32 || public.MLDSA65 == nil {
			t.Fatalf("an accepted identity is incomplete")
		}
	})
}
