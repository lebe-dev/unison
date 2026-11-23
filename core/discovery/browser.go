package discovery

import (
	"maps"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	// candidateBuffer is the number of candidates buffered per browser.
	candidateBuffer = 16
	// maxHosts bounds the address cache of a browser.
	maxHosts = 64
	// maxInstances bounds the service instances a browser remembers.
	maxInstances = 64
	// maxHostIPs bounds the addresses of one host on one interface.
	maxHostIPs = 8
	// maxRecordTTL bounds the lifetime of a record, whatever TTL it announces.
	maxRecordTTL = ttlService * time.Second
	// maxHostQueries bounds the extra address queries of one round.
	maxHostQueries = 8
)

// Candidate is a discovered service instance.
type Candidate struct {
	// Addr is the host and port of the instance.
	Addr string
	// TXT is the instance metadata.
	TXT map[string]string
	// Matched reports whether the instance tag was produced by the group key.
	Matched bool
}

// browser collects the instances of one service from mDNS responses.
type browser struct {
	service ports.Service
	typ     string
	ch      chan Candidate
	poke    chan struct{}
	match   func(txt map[string]string, tags []string) bool
	isOwn   func(name string) bool

	mu     sync.Mutex
	closed bool
	cache  browserCache
}

// handle merges the records of p and emits new candidates.
func (b *browser) handle(p packet, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}

	b.cache.update(p.msg, b.typ, ifaceName(p.iface), now)
	for _, c := range b.cache.candidates(b.typ, now, b.isOwn, b.match) {
		select {
		case b.ch <- c:
		default:
		}
	}
}

// close closes the candidate channel. Later calls are ignored.
func (b *browser) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.ch)
}

// unresolvedHosts returns the hosts of known instances without addresses.
func (b *browser) unresolvedHosts(now time.Time, limit int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	var hosts []string
	for _, inst := range b.cache.instances {
		if inst.host == "" || len(hosts) == limit {
			continue
		}
		if h, ok := b.cache.hosts[inst.host]; ok && h.valid(now) {
			continue
		}
		hosts = append(hosts, inst.host)
	}
	return hosts
}

// instance is one advertised service instance seen on the network.
type instance struct {
	port int
	host string
	txt  map[string]string
	tags []string
	seen time.Time
	ttl  time.Duration
}

// host is the address set of one announced host name, per interface.
type host struct {
	ips  map[string][]net.IP
	seen time.Time
	ttl  time.Duration
}

func (h *host) valid(now time.Time) bool {
	return len(h.ips) > 0 && !expired(now, h.seen, h.ttl)
}

// browserCache keeps the records a browser has seen.
type browserCache struct {
	instances map[string]*instance
	hosts     map[string]*host
	emitted   map[string]string
}

// update merges the records of msg for service typ into the cache.
func (b *browserCache) update(msg *dns.Msg, typ, ifaceName string, now time.Time) {
	if b.instances == nil {
		b.instances = make(map[string]*instance)
		b.hosts = make(map[string]*host)
		b.emitted = make(map[string]string)
	}

	sections := make([]dns.RR, 0, len(msg.Answer)+len(msg.Ns)+len(msg.Extra))
	sections = append(sections, msg.Answer...)
	sections = append(sections, msg.Ns...)
	sections = append(sections, msg.Extra...)

	for _, rr := range sections {
		switch rr := rr.(type) {
		case *dns.PTR:
			if !strings.EqualFold(rr.Hdr.Name, typ) {
				continue
			}
			b.instance(strings.ToLower(rr.Ptr), now, rr.Hdr.Ttl)
		case *dns.SRV:
			if !strings.HasSuffix(strings.ToLower(rr.Hdr.Name), strings.ToLower(typ)) {
				continue
			}
			inst := b.instance(strings.ToLower(rr.Hdr.Name), now, rr.Hdr.Ttl)
			inst.port = int(rr.Port)
			inst.host = strings.ToLower(rr.Target)
		case *dns.TXT:
			if !strings.HasSuffix(strings.ToLower(rr.Hdr.Name), strings.ToLower(typ)) {
				continue
			}
			inst := b.instance(strings.ToLower(rr.Hdr.Name), now, rr.Hdr.Ttl)
			inst.txt = parseTXT(rr.Txt)
			inst.tags = parseTags(rr.Txt)
		case *dns.A:
			if h := b.host(strings.ToLower(rr.Hdr.Name), now, rr.Hdr.Ttl); h != nil {
				h.ips[ifaceName] = appendIP(h.ips[ifaceName], rr.A)
			}
		case *dns.AAAA:
			if h := b.host(strings.ToLower(rr.Hdr.Name), now, rr.Hdr.Ttl); h != nil {
				h.ips[ifaceName] = appendIP(h.ips[ifaceName], rr.AAAA)
			}
		}
	}
}

// instance returns the entry of a service instance. A full cache drops the
// entry seen least recently, so a flood of the local network cannot keep a
// device that announces itself again out.
func (b *browserCache) instance(name string, now time.Time, ttl uint32) *instance {
	inst, ok := b.instances[name]
	if !ok {
		if len(b.instances) >= maxInstances {
			b.dropInstance(oldest(b.instances, func(i *instance) time.Time { return i.seen }))
		}
		inst = &instance{}
		b.instances[name] = inst
	}
	inst.seen = now
	inst.ttl = recordTTL(ttl)
	return inst
}

// host returns the address entry of name. A full cache drops the entry seen
// least recently.
func (b *browserCache) host(name string, now time.Time, ttl uint32) *host {
	h, ok := b.hosts[name]
	if !ok {
		if len(b.hosts) >= maxHosts {
			delete(b.hosts, oldest(b.hosts, func(h *host) time.Time { return h.seen }))
		}
		h = &host{ips: make(map[string][]net.IP)}
		b.hosts[name] = h
	}
	h.seen = now
	h.ttl = recordTTL(ttl)
	return h
}

// dropInstance removes an instance and the candidates emitted for it.
func (b *browserCache) dropInstance(name string) {
	delete(b.instances, name)
	prefix := name + "\x00"
	for key := range b.emitted {
		if strings.HasPrefix(key, prefix) {
			delete(b.emitted, key)
		}
	}
}

// oldest returns the key of the entry seen least recently.
func oldest[T any](entries map[string]T, seen func(T) time.Time) string {
	var name string
	var at time.Time
	for key, entry := range entries {
		if name == "" || seen(entry).Before(at) {
			name, at = key, seen(entry)
		}
	}
	return name
}

// recordTTL returns the lifetime of a record within maxRecordTTL.
func recordTTL(ttl uint32) time.Duration {
	return min(time.Duration(ttl)*time.Second, maxRecordTTL)
}

// candidates returns the addresses of the known instances that were not
// emitted with their current tag.
func (b *browserCache) candidates(typ string, now time.Time, isOwn func(string) bool, match func(map[string]string, []string) bool) []Candidate {
	var out []Candidate
	for name, inst := range b.instances {
		if !strings.HasSuffix(strings.ToLower(name), strings.ToLower(typ)) {
			continue
		}
		if expired(now, inst.seen, inst.ttl) {
			b.dropInstance(name)
			continue
		}
		if inst.port == 0 || inst.host == "" {
			continue
		}
		if isOwn != nil && isOwn(name) {
			continue
		}
		if v, ok := inst.txt[txtVersionKey]; ok && v != txtVersion {
			continue
		}

		h, ok := b.hosts[inst.host]
		if !ok || !h.valid(now) {
			continue
		}
		for ifaceName, ips := range h.ips {
			for _, ip := range ips {
				addr := address(ip, ifaceName, inst.port)
				if addr == "" {
					continue
				}
				key := name + "\x00" + addr
				signature := strings.Join(inst.tags, ",")
				if prev, ok := b.emitted[key]; ok && prev == signature {
					continue
				}
				b.emitted[key] = signature
				out = append(out, Candidate{
					Addr:    addr,
					TXT:     maps.Clone(inst.txt),
					Matched: match(inst.txt, inst.tags),
				})
			}
		}
	}

	for name, h := range b.hosts {
		if !h.valid(now) {
			delete(b.hosts, name)
		}
	}
	return out
}

// address formats a candidate address. Link local IPv6 addresses carry the
// zone of the interface they were seen on.
func address(ip net.IP, ifaceName string, port int) string {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return ""
	}
	host := ip.String()
	if ip.To4() == nil && ip.IsLinkLocalUnicast() && ifaceName != "" {
		host += "%" + ifaceName
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// appendIP adds an address once. A host keeps at most maxHostIPs addresses
// per interface.
func appendIP(ips []net.IP, ip net.IP) []net.IP {
	for _, known := range ips {
		if known.Equal(ip) {
			return ips
		}
	}
	if len(ips) >= maxHostIPs {
		return ips
	}
	return append(ips, ip)
}

// expired reports whether a record seen at seen and valid for ttl is stale.
func expired(now, seen time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return true
	}
	return now.Sub(seen) > ttl
}

func ifaceName(iface *net.Interface) string {
	if iface == nil {
		return ""
	}
	return iface.Name
}
