package discovery

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
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

// FuzzMDNSPacket feeds raw mDNS messages, which any host of the local network
// can send, to the browser cache of every service. The cache must stay within
// its bounds and emit only usable addresses.
func FuzzMDNSPacket(f *testing.F) {
	for _, count := range []int{1, 3} {
		packed, err := floodPacket(count, ttlService).Pack()
		if err != nil {
			f.Fatalf("pack: %v", err)
		}
		f.Add(packed)
	}
	f.Add([]byte{})
	f.Add(make([]byte, 12))

	types := []string{"_unison._udp.local.", "_unison-pair._udp.local.", "_unison-friends._udp.local."}
	now := time.Unix(1763000000, 0)

	f.Fuzz(func(t *testing.T, data []byte) {
		msg := new(dns.Msg)
		if err := msg.Unpack(data); err != nil {
			return
		}
		for _, typ := range types {
			var cache browserCache
			for round := range 3 {
				at := now.Add(time.Duration(round) * time.Minute)
				cache.update(msg, typ, "en0", at)
				for _, c := range cache.candidates(typ, at, nil, func(map[string]string, []string) bool { return true }) {
					host, port, err := net.SplitHostPort(c.Addr)
					if err != nil {
						t.Fatalf("candidate address %q: %v", c.Addr, err)
					}
					if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
						t.Fatalf("candidate port %q", port)
					}
					ip := net.ParseIP(strings.Split(host, "%")[0])
					if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
						t.Fatalf("candidate host %q", host)
					}
				}
			}
			if len(cache.instances) > maxInstances || len(cache.hosts) > maxHosts {
				t.Fatalf("cache of %d instances and %d hosts", len(cache.instances), len(cache.hosts))
			}
			for name, h := range cache.hosts {
				for iface, ips := range h.ips {
					if len(ips) > maxHostIPs {
						t.Fatalf("host %s holds %d addresses on %s", name, len(ips), iface)
					}
				}
			}
		}
	})
}
