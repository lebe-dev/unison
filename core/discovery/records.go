package discovery

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/miekg/dns"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	// dnsDomain is the DNS-SD domain used on the local network.
	dnsDomain = "local."
	// ttlService is the TTL of PTR and TXT records.
	ttlService = 4500
	// ttlHost is the TTL of SRV, A and AAAA records.
	ttlHost = 120
	// flushBit marks a record as replacing the cached one.
	flushBit = 1 << 15
)

// serviceType returns the DNS-SD type of service.
func serviceType(service ports.Service) (string, error) {
	switch service {
	case ports.ServiceMain:
		return "_unison._udp." + dnsDomain, nil
	case ports.ServicePairing:
		return "_unison-pair._udp." + dnsDomain, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownService, service)
	}
}

// recordSet describes one advertised service instance.
type recordSet struct {
	typ      string
	instance string
	host     string
	port     int
	txt      map[string]string
}

// fqdn returns the fully qualified instance name.
func (r recordSet) fqdn() string {
	return r.instance + "." + r.typ
}

func (r recordSet) ptr(ttl uint32) *dns.PTR {
	return &dns.PTR{
		Hdr: dns.RR_Header{Name: r.typ, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl},
		Ptr: r.fqdn(),
	}
}

func (r recordSet) srv(ttl uint32) *dns.SRV {
	return &dns.SRV{
		Hdr:      uniqueHeader(r.fqdn(), dns.TypeSRV, ttl),
		Priority: 0,
		Weight:   0,
		Port:     uint16(r.port),
		Target:   r.host,
	}
}

func (r recordSet) txtRecord(ttl uint32) *dns.TXT {
	keys := make([]string, 0, len(r.txt))
	for k := range r.txt {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	txt := make([]string, 0, len(keys))
	for _, k := range keys {
		txt = append(txt, k+"="+r.txt[k])
	}
	return &dns.TXT{
		Hdr: uniqueHeader(r.fqdn(), dns.TypeTXT, ttl),
		Txt: txt,
	}
}

// announcement returns the unsolicited response for iface. Expired records
// withdraw the instance.
func (r recordSet) announcement(iface *net.Interface, expire bool) *dns.Msg {
	serviceTTL, hostTTL := uint32(ttlService), uint32(ttlHost)
	if expire {
		serviceTTL, hostTTL = 0, 0
	}

	msg := &dns.Msg{
		MsgHdr:   dns.MsgHdr{Response: true, Authoritative: true},
		Compress: true,
	}
	msg.Answer = []dns.RR{r.ptr(serviceTTL)}
	msg.Extra = append(msg.Extra, r.srv(hostTTL), r.txtRecord(serviceTTL))
	msg.Extra = append(msg.Extra, addrs(r.host, iface, hostTTL, !expire)...)
	return msg
}

// addrs returns the A and AAAA records of host on iface.
func addrs(host string, iface *net.Interface, ttl uint32, flush bool) []dns.RR {
	if iface == nil {
		return nil
	}
	list, err := iface.Addrs()
	if err != nil {
		return nil
	}

	var records []dns.RR
	for _, a := range list {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil {
			continue
		}
		if ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}

		hdr := dns.RR_Header{Name: host, Class: dns.ClassINET, Ttl: ttl}
		if flush && ttl > 0 {
			hdr.Class |= flushBit
		}
		if v4 := ip.To4(); v4 != nil {
			hdr.Rrtype = dns.TypeA
			records = append(records, &dns.A{Hdr: hdr, A: v4})
			continue
		}
		hdr.Rrtype = dns.TypeAAAA
		records = append(records, &dns.AAAA{Hdr: hdr, AAAA: ip.To16()})
	}
	return records
}

// uniqueHeader returns the header of a unique record.
func uniqueHeader(name string, rrtype uint16, ttl uint32) dns.RR_Header {
	hdr := dns.RR_Header{Name: name, Rrtype: rrtype, Class: dns.ClassINET, Ttl: ttl}
	if ttl > 0 {
		hdr.Class |= flushBit
	}
	return hdr
}

// query returns a multicast query for name.
func query(name string) *dns.Msg {
	return &dns.Msg{
		MsgHdr: dns.MsgHdr{Id: 0, RecursionDesired: false},
		Question: []dns.Question{{
			Name:   name,
			Qtype:  dns.TypePTR,
			Qclass: dns.ClassINET,
		}},
	}
}

// addrQuery returns a multicast query for the addresses of name.
func addrQuery(name string) *dns.Msg {
	return &dns.Msg{
		MsgHdr: dns.MsgHdr{Id: 0, RecursionDesired: false},
		Question: []dns.Question{
			{Name: name, Qtype: dns.TypeA, Qclass: dns.ClassINET},
			{Name: name, Qtype: dns.TypeAAAA, Qclass: dns.ClassINET},
		},
	}
}

// parseTXT converts TXT strings to a map. Entries without a key are dropped.
func parseTXT(txt []string) map[string]string {
	if len(txt) == 0 {
		return nil
	}
	fields := make(map[string]string, len(txt))
	for _, s := range txt {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			continue
		}
		fields[k] = v
	}
	return fields
}
