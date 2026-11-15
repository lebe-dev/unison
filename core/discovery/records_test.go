package discovery

import (
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/lebe-dev/unison/core/ports"
)

func TestServiceType(t *testing.T) {
	cases := []struct {
		service ports.Service
		want    string
	}{
		{ports.ServiceMain, "_unison._udp.local."},
		{ports.ServicePairing, "_unison-pair._udp.local."},
	}
	for _, c := range cases {
		got, err := serviceType(c.service)
		if err != nil {
			t.Fatalf("serviceType(%q): %v", c.service, err)
		}
		if got != c.want {
			t.Errorf("serviceType(%q) = %q, want %q", c.service, got, c.want)
		}
	}

	if _, err := serviceType("other"); err == nil {
		t.Fatal("serviceType accepted an unknown service")
	}
}

func TestAnnouncement(t *testing.T) {
	rs := recordSet{
		typ:      "_unison._udp.local.",
		instance: "abcdef",
		host:     "unison-abcdef.local.",
		port:     4242,
		txt:      map[string]string{"v": "1", "tag": "r2ffkbe3kxzai"},
	}

	records := rs.announcement(nil, false)
	if records == nil {
		t.Fatal("no announcement")
	}
	if got := records.Answer; len(got) != 1 || got[0].Header().Rrtype != dns.TypePTR {
		t.Fatalf("answer = %v, want one PTR record", got)
	}

	var srv *dns.SRV
	var txt *dns.TXT
	for _, rr := range records.Extra {
		switch rr := rr.(type) {
		case *dns.SRV:
			srv = rr
		case *dns.TXT:
			txt = rr
		}
	}
	if srv == nil || srv.Port != 4242 || srv.Target != rs.host {
		t.Fatalf("SRV = %v, want port 4242 and target %q", srv, rs.host)
	}
	if txt == nil {
		t.Fatal("no TXT record")
	}
	if len(txt.Txt) != 2 || txt.Txt[0] != "tag=r2ffkbe3kxzai" || txt.Txt[1] != "v=1" {
		t.Fatalf("TXT = %v, want sorted key=value fields", txt.Txt)
	}

	expired := rs.announcement(nil, true)
	for _, rr := range expired.Extra {
		if rr.Header().Ttl != 0 {
			t.Fatalf("expired record %v has ttl %d", rr, rr.Header().Ttl)
		}
	}
}

func TestAddrs(t *testing.T) {
	iface, err := net.InterfaceByName("lo0")
	if err != nil {
		t.Skip("no loopback interface")
	}

	records := addrs("unison-abcdef.local.", iface, ttlHost, true)
	if len(records) == 0 {
		t.Fatal("no address records for the loopback interface")
	}
	for _, rr := range records {
		if rr.Header().Name != "unison-abcdef.local." {
			t.Errorf("record %v has name %q", rr, rr.Header().Name)
		}
		if rr.Header().Class&flushBit == 0 {
			t.Errorf("record %v is not marked as unique", rr)
		}
	}
}

func TestParseTXT(t *testing.T) {
	txt := parseTXT([]string{"v=1", "tag=abc", "novalue", "=broken", "k=a=b"})
	want := map[string]string{"v": "1", "tag": "abc", "k": "a=b"}
	if len(txt) != len(want) {
		t.Fatalf("parseTXT = %v, want %v", txt, want)
	}
	for k, v := range want {
		if txt[k] != v {
			t.Errorf("parseTXT[%q] = %q, want %q", k, txt[k], v)
		}
	}
	if parseTXT(nil) != nil {
		t.Error("parseTXT(nil) returned a map")
	}
}
