package discovery

import (
	"strings"
	"testing"
	"time"
)

// FuzzParseTXT checks that an arbitrary TXT record set never produces fields
// that the tag check accepts by accident.
func FuzzParseTXT(f *testing.F) {
	f.Add("v=1\x00tag=abcdefghijklm")
	f.Add("")
	f.Add("=\x00tag=\x00v=1")
	f.Add("tag=abcdefghijklm\x00tag=abcdefghijklm")

	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1763000000, 0)

	f.Fuzz(func(t *testing.T, data string) {
		entries := strings.Split(data, "\x00")
		fields := parseTXT(entries)

		want := 0
		for _, entry := range entries {
			if name, _, ok := strings.Cut(entry, "="); ok && name != "" {
				want++
			}
		}
		if len(fields) == 0 && want > 0 {
			t.Fatalf("%d entries produced no fields", want)
		}
		if len(fields) > want {
			t.Fatalf("%d entries produced %d fields", want, len(fields))
		}

		tag := fields[txtTagKey]
		if MatchAnyTag([][]byte{key}, tag, now) && len(strings.ToLower(tag)) != TagLength {
			t.Fatalf("accepted the tag %q", tag)
		}
		if !MatchAnyTag([][]byte{key}, Tag(key, now), now) {
			t.Fatalf("the tag of the key does not match")
		}
	})
}
