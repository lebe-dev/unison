package discovery

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const boundsType = "_unison._udp.local."

// floodPacket builds a response with count instances, each with its own host.
func floodPacket(count int, ttl uint32) *dns.Msg {
	msg := &dns.Msg{MsgHdr: dns.MsgHdr{Response: true}}
	for i := range count {
		instance := fmt.Sprintf("device-%d.%s", i, boundsType)
		host := fmt.Sprintf("host-%d.local.", i)
		msg.Answer = append(msg.Answer,
			&dns.PTR{Hdr: dns.RR_Header{Name: boundsType, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl}, Ptr: instance},
			&dns.SRV{Hdr: dns.RR_Header{Name: instance, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: ttl}, Port: 7000, Target: host},
			&dns.TXT{Hdr: dns.RR_Header{Name: instance, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttl}, Txt: []string{"v=1"}},
			&dns.A{Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}, A: net.IPv4(10, 0, byte(i>>8), byte(i))},
		)
	}
	return msg
}

// TestBrowserCacheBoundsInstances checks that a flood of announcements from
// the local network cannot grow the cache without limit.
func TestBrowserCacheBoundsInstances(t *testing.T) {
	var cache browserCache
	now := time.Unix(1763000000, 0)
	for round := range 20 {
		cache.update(floodPacket(500, ttlService), boundsType, "en0", now.Add(time.Duration(round)*time.Second))
		cache.candidates(boundsType, now, nil, func(map[string]string, []string) bool { return false })
	}
	if len(cache.instances) > maxInstances {
		t.Fatalf("the cache holds %d instances, limit is %d", len(cache.instances), maxInstances)
	}
	if len(cache.hosts) > maxHosts {
		t.Fatalf("the cache holds %d hosts, limit is %d", len(cache.hosts), maxHosts)
	}
	if len(cache.emitted) > maxInstances*maxHostIPs {
		t.Fatalf("the cache remembers %d emitted candidates", len(cache.emitted))
	}
}

// TestBrowserCacheBoundsAddresses checks the addresses of one host.
func TestBrowserCacheBoundsAddresses(t *testing.T) {
	msg := &dns.Msg{MsgHdr: dns.MsgHdr{Response: true}}
	for i := range 1000 {
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: "host.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttlHost},
			A:   net.IPv4(10, 1, byte(i>>8), byte(i)),
		})
	}
	var cache browserCache
	cache.update(msg, boundsType, "en0", time.Unix(1763000000, 0))
	if got := len(cache.hosts["host.local."].ips["en0"]); got > maxHostIPs {
		t.Fatalf("the host holds %d addresses, limit is %d", got, maxHostIPs)
	}
}

// TestBrowserCacheClampsTTL checks that a record cannot stay forever by
// announcing a huge TTL.
func TestBrowserCacheClampsTTL(t *testing.T) {
	var cache browserCache
	now := time.Unix(1763000000, 0)
	cache.update(floodPacket(1, ^uint32(0)), boundsType, "en0", now)

	later := now.Add(maxRecordTTL + time.Second)
	cache.candidates(boundsType, later, nil, func(map[string]string, []string) bool { return false })
	if len(cache.instances) != 0 || len(cache.hosts) != 0 {
		t.Fatalf("records with a huge TTL survived: %d instances, %d hosts", len(cache.instances), len(cache.hosts))
	}
}
