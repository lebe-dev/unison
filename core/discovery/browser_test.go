package discovery

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// response builds an mDNS response for one instance.
func response(typ string, txt map[string]string, ips ...net.IP) *dns.Msg {
	const host = "unison-abcdef.local."
	const port = 4242
	fqdn := "abcdef." + typ
	msg := &dns.Msg{MsgHdr: dns.MsgHdr{Response: true, Authoritative: true}}
	msg.Answer = []dns.RR{
		&dns.PTR{
			Hdr: dns.RR_Header{Name: typ, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 120},
			Ptr: fqdn,
		},
	}
	msg.Extra = []dns.RR{
		&dns.SRV{
			Hdr:    dns.RR_Header{Name: fqdn, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120},
			Port:   uint16(port),
			Target: host,
		},
		&dns.TXT{
			Hdr: dns.RR_Header{Name: fqdn, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 120},
			Txt: txtFields(txt),
		},
	}
	for _, ip := range ips {
		hdr := dns.RR_Header{Name: host, Class: dns.ClassINET, Ttl: 120}
		if v4 := ip.To4(); v4 != nil {
			hdr.Rrtype = dns.TypeA
			msg.Extra = append(msg.Extra, &dns.A{Hdr: hdr, A: v4})
			continue
		}
		hdr.Rrtype = dns.TypeAAAA
		msg.Extra = append(msg.Extra, &dns.AAAA{Hdr: hdr, AAAA: ip})
	}
	return msg
}

func txtFields(txt map[string]string) []string {
	fields := make([]string, 0, len(txt))
	for k, v := range txt {
		fields = append(fields, k+"="+v)
	}
	return fields
}

func matchAll(map[string]string, []string) bool { return true }

func TestBrowserCandidates(t *testing.T) {
	now := epochTime(1000)
	cache := &browserCache{}
	msg := response("_unison._udp.local.",
		map[string]string{"v": "1", "tag": "r2ffkbe3kxzai"},
		net.ParseIP("192.0.2.10"), net.ParseIP("fe80::1"))

	cache.update(msg, "_unison._udp.local.", "en0", now)
	got := cache.candidates("_unison._udp.local.", now, nil, matchAll)
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2: %v", len(got), got)
	}

	addrs := map[string]string{}
	for _, c := range got {
		addrs[c.Addr] = c.TXT["tag"]
	}
	if addrs["192.0.2.10:4242"] != "r2ffkbe3kxzai" {
		t.Errorf("IPv4 candidate = %q", addrs["192.0.2.10:4242"])
	}
	if addrs["[fe80::1%en0]:4242"] != "r2ffkbe3kxzai" {
		t.Errorf("IPv6 candidate = %q", addrs["[fe80::1%en0]:4242"])
	}

	if again := cache.candidates("_unison._udp.local.", now, nil, matchAll); len(again) != 0 {
		t.Fatalf("unchanged records produced %d candidates again", len(again))
	}
}

func TestBrowserTagChange(t *testing.T) {
	now := epochTime(1000)
	cache := &browserCache{}
	typ := "_unison._udp.local."
	ip := net.ParseIP("192.0.2.10")

	cache.update(response(typ,
		map[string]string{"v": "1", "tag": "r2ffkbe3kxzai"}, ip), typ, "en0", now)
	if got := cache.candidates(typ, now, nil, matchAll); len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}

	cache.update(response(typ,
		map[string]string{"v": "1", "tag": "lm6xtpxkblwgm"}, ip), typ, "en0", now)
	got := cache.candidates(typ, now, nil, matchAll)
	if len(got) != 1 || got[0].TXT["tag"] != "lm6xtpxkblwgm" {
		t.Fatalf("tag change produced %v, want one candidate with the new tag", got)
	}
}

func TestBrowserFilters(t *testing.T) {
	now := epochTime(1000)
	typ := "_unison._udp.local."
	ip := net.ParseIP("192.0.2.10")

	cache := &browserCache{}
	cache.update(response(typ,
		map[string]string{"v": "1", "tag": "r2ffkbe3kxzai"}, ip), typ, "en0", now)

	if got := cache.candidates(typ, now, func(string) bool { return true }, matchAll); len(got) != 0 {
		t.Fatalf("own instance produced %d candidates", len(got))
	}
	if got := cache.candidates(typ, now, nil, func(map[string]string, []string) bool { return false }); len(got) != 1 || got[0].Matched {
		t.Fatalf("unmatched instance produced %v", got)
	}

	other := &browserCache{}
	other.update(response(typ,
		map[string]string{"v": "2"}, ip), typ, "en0", now)
	if got := other.candidates(typ, now, nil, matchAll); len(got) != 0 {
		t.Fatalf("instance with another version produced %d candidates", len(got))
	}
}

func TestBrowserExpiry(t *testing.T) {
	now := epochTime(1000)
	typ := "_unison._udp.local."
	cache := &browserCache{}
	cache.update(response(typ,
		map[string]string{"v": "1"}, net.ParseIP("192.0.2.10")), typ, "en0", now)

	later := now.Add(3 * time.Minute)
	if got := cache.candidates(typ, later, nil, matchAll); len(got) != 0 {
		t.Fatalf("stale records produced %d candidates", len(got))
	}
	if len(cache.instances) != 0 || len(cache.hosts) != 0 {
		t.Fatalf("stale records were not removed: %d instances, %d hosts", len(cache.instances), len(cache.hosts))
	}
}

func TestBrowserOtherService(t *testing.T) {
	now := epochTime(1000)
	pairing := "_unison-pair._udp.local."
	cache := &browserCache{}
	cache.update(response(pairing, map[string]string{"v": "1"}, net.ParseIP("192.0.2.10")), pairing, "en0", now)

	if got := cache.candidates("_unison._udp.local.", now, nil, matchAll); len(got) != 0 {
		t.Fatalf("pairing instance produced %d main candidates", len(got))
	}
	if got := cache.candidates("_unison-pair._udp.local.", now, nil, matchAll); len(got) != 1 {
		t.Fatalf("got %d pairing candidates, want 1", len(got))
	}
}

func TestAddress(t *testing.T) {
	cases := []struct {
		ip   string
		port int
		want string
	}{
		{"192.0.2.10", 4242, "192.0.2.10:4242"},
		{"2001:db8::1", 4242, "[2001:db8::1]:4242"},
		{"fe80::1", 4242, "[fe80::1%en0]:4242"},
		{"0.0.0.0", 4242, ""},
		{"224.0.0.251", 4242, ""},
	}
	for _, c := range cases {
		if got := address(net.ParseIP(c.ip), "en0", c.port); got != c.want {
			t.Errorf("address(%s) = %q, want %q", c.ip, got, c.want)
		}
	}
}
