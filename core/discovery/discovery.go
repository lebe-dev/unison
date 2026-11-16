package discovery

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/lebe-dev/unison/core/ports"
)

const (
	// DefaultRefresh is how often the tag epoch and the interface set are
	// checked.
	DefaultRefresh = 30 * time.Second
	// DefaultQueryInterval is how often a browser repeats its query.
	DefaultQueryInterval = 5 * time.Second
	// announceRepeat is the delay before the second announcement.
	announceRepeat = time.Second
	// keyCacheTTL is how long the group key is cached.
	keyCacheTTL = time.Minute
	// keyRetryTTL is how long an absent group key is cached before it is read
	// again.
	keyRetryTTL = 5 * time.Second
	// Reserved TXT keys and their values.
	txtVersionKey = "v"
	txtTagKey     = "tag"
	txtVersion    = "1"
)

// Errors of the discovery package.
var (
	ErrStopped        = errors.New("discovery: stopped")
	ErrUnknownService = errors.New("discovery: unknown service")
	ErrNoInterfaces   = errors.New("discovery: no multicast interface")
)

// Options configures a Discovery.
type Options struct {
	// Key returns the discovery key of the Personal group. Instances are
	// advertised without a tag when it returns nil.
	Key func(ctx context.Context) ([]byte, error)
	// Keys returns the keys that match the group tag: the current one first
	// and the previous one of a rotation while it still matches. Key is used
	// when nil.
	Keys func(ctx context.Context) ([][]byte, error)
	// FriendsKeys returns the rendezvous keys of the Friends devices. Their
	// tags are announced on the friends service, and a device that looks for
	// friends matches them with the same keys. Nothing is advertised for
	// Friends when it returns nil.
	FriendsKeys func(ctx context.Context) ([][]byte, error)
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// Logger receives discovery events. Nothing is logged when nil.
	Logger *slog.Logger
	// Ifaces restricts discovery to the given interfaces. Every multicast
	// interface is used when nil.
	Ifaces []net.Interface
	// Refresh is how often the tag epoch and the interface set are checked.
	// DefaultRefresh is used when zero.
	Refresh time.Duration
	// QueryInterval is how often a browser repeats its query.
	// DefaultQueryInterval is used when zero.
	QueryInterval time.Duration
}

// advert is the desired announcement of one service.
type advert struct {
	service  ports.Service
	instance string
	port     int
	txt      map[string]string
}

// fqdn returns the fully qualified instance name.
func (a advert) fqdn() string {
	typ, _ := serviceType(a.service)
	return a.instance + "." + typ
}

// keyCache holds the keys of one source.
type keyCache struct {
	keys  [][]byte
	until time.Time
}

// Discovery advertises and browses services on the local network.
type Discovery struct {
	opts  Options
	clock ports.Clock
	log   *slog.Logger
	host  string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	ads       map[ports.Service]*advert
	browsers  map[int]*browser
	nextID    int
	tr        *transport
	ifaceHash string
	// access follows whether the system lets the sends reach the local network.
	access *lanAccess
	// lanWarned is set while the refused local network has been logged.
	lanWarned atomic.Bool
	epoch     int64
	group     keyCache
	friends   keyCache
	stopped   bool
}

var _ ports.Discovery = (*Discovery)(nil)

// New creates a Discovery. Stop releases its resources.
func New(opts Options) (*Discovery, error) {
	if opts.Clock == nil {
		opts.Clock = ports.SystemClock
	}
	if opts.Refresh <= 0 {
		opts.Refresh = DefaultRefresh
	}
	if opts.QueryInterval <= 0 {
		opts.QueryInterval = DefaultQueryInterval
	}

	label, err := randomLabel()
	if err != nil {
		return nil, err
	}

	d := &Discovery{
		opts:     opts,
		clock:    opts.Clock,
		log:      opts.Logger,
		host:     "unison-" + label + "." + dnsDomain,
		ads:      make(map[ports.Service]*advert),
		browsers: make(map[int]*browser),
	}
	d.access = newLANAccess(d.clock.Now)
	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.epoch = Epoch(d.clock.Now())
	d.opts.Ifaces = cloneIfaces(opts.Ifaces)

	d.wg.Add(1)
	go d.refreshLoop()
	return d, nil
}

// Advertise announces service on port with txt. Advertising the same service
// again replaces the previous announcement.
func (d *Discovery) Advertise(ctx context.Context, service ports.Service, port int, txt map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := serviceType(service); err != nil {
		return err
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("discovery: invalid port %d", port)
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return ErrStopped
	}
	ad, ok := d.ads[service]
	if !ok {
		label, err := randomLabel()
		if err != nil {
			d.mu.Unlock()
			return err
		}
		ad = &advert{service: service, instance: label}
		d.ads[service] = ad
	}
	ad.port = port
	ad.txt = maps.Clone(txt)
	delete(ad.txt, txtVersionKey)
	delete(ad.txt, txtTagKey)
	d.mu.Unlock()

	if err := d.ensureTransport(); err != nil {
		return err
	}
	d.announce(ctx, *ad)
	return nil
}

// Browse delivers the instances of service until ctx is done or the Discovery
// stops. The channel is closed when browsing ends.
func (d *Discovery) Browse(ctx context.Context, service ports.Service) (<-chan ports.Candidate, error) {
	tagged, err := d.BrowseCandidates(ctx, service)
	if err != nil {
		return nil, err
	}

	out := make(chan ports.Candidate, candidateBuffer)
	go func() {
		defer close(out)
		for c := range tagged {
			select {
			case out <- ports.Candidate{Addr: c.Addr, TXT: c.TXT}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// BrowseCandidates delivers the instances of service with the tag verdict.
// Candidates are repeated when their TXT records change.
func (d *Discovery) BrowseCandidates(ctx context.Context, service ports.Service) (<-chan Candidate, error) {
	typ, err := serviceType(service)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return nil, ErrStopped
	}
	if err := d.transportLocked(); err != nil {
		d.mu.Unlock()
		return nil, err
	}
	id := d.nextID
	d.nextID++
	b := &browser{
		service: service,
		typ:     typ,
		ch:      make(chan Candidate, candidateBuffer),
		poke:    make(chan struct{}, 1),
		match:   func(_ map[string]string, tags []string) bool { return d.matchTags(service, tags) },
		isOwn:   d.isOwn,
	}
	d.browsers[id] = b
	d.wg.Add(1)
	d.mu.Unlock()

	go d.browse(ctx, id, b)
	return b.ch, nil
}

// StopService withdraws the announcement of service. Other services and the
// running browsers are not affected.
func (d *Discovery) StopService(service ports.Service) error {
	d.mu.Lock()
	ad, ok := d.ads[service]
	if ok {
		delete(d.ads, service)
	}
	tr := d.tr
	d.mu.Unlock()

	if !ok || tr == nil {
		return nil
	}
	d.expire(*ad, tr)
	return nil
}

// Stop withdraws every announcement and closes every browse channel.
func (d *Discovery) Stop() error {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return nil
	}
	d.stopped = true
	ads := make([]advert, 0, len(d.ads))
	for _, ad := range d.ads {
		ads = append(ads, *ad)
	}
	tr := d.tr
	d.mu.Unlock()

	if tr != nil {
		for _, ad := range ads {
			d.expire(ad, tr)
		}
	}

	d.cancel()
	if tr != nil {
		tr.close()
	}
	d.wg.Wait()
	return nil
}

// refreshLoop re-announces on tag epoch changes and on network changes.
func (d *Discovery) refreshLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.opts.Refresh)
	defer ticker.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
		}
		d.refresh()
	}
}

// refresh brings the sockets and the announcements up to date.
func (d *Discovery) refresh() {
	ifaces := d.interfaces()
	hash := ifaceHash(ifaces)
	epoch := Epoch(d.clock.Now())

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	epochChanged := epoch != d.epoch
	ifaceChanged := hash != d.ifaceHash
	if !epochChanged && !ifaceChanged {
		d.mu.Unlock()
		return
	}
	d.epoch = epoch
	if ifaceChanged {
		d.ifaceHash = hash
		if d.tr != nil {
			d.tr.close()
			d.tr = nil
		}
	}
	ads := make([]advert, 0, len(d.ads))
	for _, ad := range d.ads {
		ads = append(ads, *ad)
	}
	browsers := make([]*browser, 0, len(d.browsers))
	for _, b := range d.browsers {
		browsers = append(browsers, b)
	}
	d.mu.Unlock()

	if err := d.ensureTransport(); err != nil {
		debugf(d.log, "open mDNS sockets: %v", err)
	}
	for _, ad := range ads {
		d.announce(d.ctx, ad)
	}
	for _, b := range browsers {
		select {
		case b.poke <- struct{}{}:
		default:
		}
	}
}

// announce sends the announcement of ad twice on every interface.
func (d *Discovery) announce(ctx context.Context, ad advert) {
	d.mu.Lock()
	tr := d.tr
	d.mu.Unlock()
	if tr == nil {
		return
	}

	rs := d.records(ctx, ad)
	for i := range tr.ifaces {
		d.send(tr, &tr.ifaces[i], rs.announcement(&tr.ifaces[i], false))
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.wg.Add(1)
	d.mu.Unlock()

	go func() {
		defer d.wg.Done()
		select {
		case <-time.After(announceRepeat):
		case <-d.ctx.Done():
			return
		}
		for i := range tr.ifaces {
			d.send(tr, &tr.ifaces[i], rs.announcement(&tr.ifaces[i], false))
		}
	}()
}

// expire sends a goodbye for ad.
func (d *Discovery) expire(ad advert, tr *transport) {
	rs := d.records(context.Background(), ad)
	for i := range tr.ifaces {
		d.send(tr, &tr.ifaces[i], rs.announcement(&tr.ifaces[i], true))
	}
}

// LocalNetworkBlocked reports that the system refuses the sends of this
// process to the local network, while the loopback interface still works. On
// macOS it means that the process has no local network permission.
func (d *Discovery) LocalNetworkBlocked() bool {
	return d.access.blocked()
}

// send writes msg on iface and logs failures at debug level.
func (d *Discovery) send(tr *transport, iface *net.Interface, msg *dns.Msg) {
	if err := tr.send(iface, msg); err != nil {
		debugf(d.log, "send mDNS message on %s: %v", ifaceName(iface), err)
	}
	d.logLocalNetwork()
}

// logLocalNetwork logs once when the system starts and stops refusing the
// local network.
func (d *Discovery) logLocalNetwork() {
	if d.log == nil {
		return
	}
	blocked := d.access.blocked()
	if blocked == d.lanWarned.Load() || !d.lanWarned.CompareAndSwap(!blocked, blocked) {
		return
	}
	if blocked {
		d.log.Warn("the system refuses access to the local network, so other devices cannot find this one; " +
			"on macOS allow it in System Settings > Privacy & Security > Local Network")
		return
	}
	d.log.Info("access to the local network is back")
}

// records returns the records of ad at the current time.
func (d *Discovery) records(ctx context.Context, ad advert) recordSet {
	txt := maps.Clone(ad.txt)
	if txt == nil {
		txt = make(map[string]string, 2)
	}
	txt[txtVersionKey] = txtVersion
	delete(txt, txtTagKey)

	rs := recordSet{
		typ:      mustServiceType(ad.service),
		instance: ad.instance,
		host:     d.host,
		port:     ad.port,
		txt:      txt,
		tags:     d.tags(ctx, ad.service),
	}
	return rs
}

// tags returns the tags of the announcement of service at the current time.
func (d *Discovery) tags(ctx context.Context, service ports.Service) []string {
	var keys [][]byte
	switch service {
	case ports.ServiceMain:
		keys = d.groupKeys(ctx)
	case ports.ServiceFriends:
		keys = d.friendsKeys(ctx)
	default:
		return nil
	}

	now := d.clock.Now()
	tags := make([]string, 0, len(keys))
	for _, key := range keys {
		if tag := Tag(key, now); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// mustServiceType returns the DNS-SD type of a service accepted by Advertise.
func mustServiceType(service ports.Service) string {
	typ, _ := serviceType(service)
	return typ
}

// groupKeys returns the keys of the Personal group: the current one first and
// the previous one of a rotation while it still matches.
func (d *Discovery) groupKeys(ctx context.Context) [][]byte {
	if d.opts.Key == nil && d.opts.Keys == nil {
		return nil
	}
	return d.cached(ctx, &d.group, d.readGroupKeys, "discovery key")
}

// friendsKeys returns the rendezvous keys of the Friends devices.
func (d *Discovery) friendsKeys(ctx context.Context) [][]byte {
	if d.opts.FriendsKeys == nil {
		return nil
	}
	return d.cached(ctx, &d.friends, d.opts.FriendsKeys, "friends discovery keys")
}

// cached returns the keys of one source. An absent key is cached for a shorter
// time than a present one.
func (d *Discovery) cached(ctx context.Context, cache *keyCache, source func(context.Context) ([][]byte, error), name string) [][]byte {
	now := d.clock.Now()
	d.mu.Lock()
	if now.Before(cache.until) {
		keys := cache.keys
		d.mu.Unlock()
		return keys
	}
	d.mu.Unlock()

	keys, err := source(ctx)
	if err != nil {
		debugf(d.log, "read %s: %v", name, err)
		keys = nil
	}

	d.mu.Lock()
	cache.keys = keys
	if len(keys) == 0 {
		cache.until = now.Add(keyRetryTTL)
	} else {
		cache.until = now.Add(keyCacheTTL)
	}
	d.mu.Unlock()
	return keys
}

// readGroupKeys asks the key sources of the option for the group keys.
func (d *Discovery) readGroupKeys(ctx context.Context) ([][]byte, error) {
	if d.opts.Keys != nil {
		keys, err := d.opts.Keys(ctx)
		if err != nil {
			return nil, err
		}
		return slices.Clone(keys), nil
	}
	key, err := d.opts.Key(ctx)
	if err != nil || len(key) == 0 {
		return nil, err
	}
	return [][]byte{key}, nil
}

// matchTags reports whether a candidate carries a tag of the service.
func (d *Discovery) matchTags(service ports.Service, tags []string) bool {
	if len(tags) == 0 {
		return false
	}
	keys := d.groupKeys(context.Background())
	if service == ports.ServiceFriends {
		keys = d.friendsKeys(context.Background())
	}

	now := d.clock.Now()
	for _, tag := range tags {
		if MatchAnyTag(keys, tag, now) {
			return true
		}
	}
	return false
}

// isOwn reports whether name is an instance advertised by this node.
func (d *Discovery) isOwn(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ad := range d.ads {
		if strings.EqualFold(ad.fqdn(), name) {
			return true
		}
	}
	return false
}

// browse queries service and emits the discovered instances.
func (d *Discovery) browse(ctx context.Context, id int, b *browser) {
	defer d.wg.Done()
	defer func() {
		d.mu.Lock()
		delete(d.browsers, id)
		d.mu.Unlock()
		b.close()
	}()

	ticker := time.NewTicker(d.opts.QueryInterval)
	defer ticker.Stop()

	d.query(b)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
		case <-b.poke:
		}
		d.query(b)
	}
}

// query sends the browse query and the address queries of unresolved hosts.
func (d *Discovery) query(b *browser) {
	d.mu.Lock()
	tr := d.tr
	d.mu.Unlock()
	if tr == nil {
		return
	}

	msg := query(b.typ)
	for i := range tr.ifaces {
		d.send(tr, &tr.ifaces[i], msg)
	}

	for _, host := range b.unresolvedHosts(d.clock.Now(), maxHostQueries) {
		addrMsg := addrQuery(host)
		for i := range tr.ifaces {
			d.send(tr, &tr.ifaces[i], addrMsg)
		}
	}
}

// dispatch handles the messages received by tr.
func (d *Discovery) dispatch(tr *transport) {
	defer d.wg.Done()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-tr.done:
			return
		case p := <-tr.packets:
			if p.msg.Response {
				d.deliver(p)
				continue
			}
			d.handleQuery(tr, p)
		}
	}
}

// deliver passes a response to every browser.
func (d *Discovery) deliver(p packet) {
	d.mu.Lock()
	browsers := make([]*browser, 0, len(d.browsers))
	for _, b := range d.browsers {
		browsers = append(browsers, b)
	}
	d.mu.Unlock()

	now := d.clock.Now()
	for _, b := range browsers {
		b.handle(p, now)
	}
}

// handleQuery answers a query for the advertised services.
func (d *Discovery) handleQuery(tr *transport, p packet) {
	if p.msg.Opcode != dns.OpcodeQuery {
		return
	}

	d.mu.Lock()
	ads := make([]advert, 0, len(d.ads))
	for _, ad := range d.ads {
		ads = append(ads, *ad)
	}
	d.mu.Unlock()
	if len(ads) == 0 {
		return
	}

	resp := d.answer(ads, p)
	if resp == nil {
		return
	}
	if err := tr.reply(p.iface, p.from, resp); err != nil {
		debugf(d.log, "answer mDNS query: %v", err)
	}
}

// answer builds the response to the questions of p.
func (d *Discovery) answer(ads []advert, p packet) *dns.Msg {
	legacy := p.from != nil && p.from.Port != mdnsPort
	resp := &dns.Msg{
		MsgHdr:   dns.MsgHdr{Response: true, Authoritative: true},
		Compress: true,
	}
	if legacy {
		resp.Id = p.msg.Id
	}

	ctx := context.Background()
	for _, q := range p.msg.Question {
		name := strings.ToLower(q.Name)
		for _, ad := range ads {
			rs := d.records(ctx, ad)
			switch name {
			case strings.ToLower(rs.typ):
				if q.Qtype != dns.TypePTR && q.Qtype != dns.TypeANY {
					continue
				}
				resp.Answer = append(resp.Answer, rs.ptr(ttlService))
				resp.Extra = append(resp.Extra, rs.srv(ttlHost), rs.txtRecord(ttlService))
				resp.Extra = append(resp.Extra, addrs(rs.host, p.iface, ttlHost, false)...)
			case strings.ToLower(rs.fqdn()):
				if q.Qtype == dns.TypeSRV || q.Qtype == dns.TypeANY {
					resp.Answer = append(resp.Answer, rs.srv(ttlHost))
				}
				if q.Qtype == dns.TypeTXT || q.Qtype == dns.TypeANY {
					resp.Answer = append(resp.Answer, rs.txtRecord(ttlService))
				}
				resp.Extra = append(resp.Extra, addrs(rs.host, p.iface, ttlHost, false)...)
			case strings.ToLower(rs.host):
				if q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA && q.Qtype != dns.TypeANY {
					continue
				}
				resp.Answer = append(resp.Answer, addrs(rs.host, p.iface, ttlHost, false)...)
			}
		}
	}

	if len(resp.Answer) == 0 {
		return nil
	}
	return resp
}

// ensureTransport opens the mDNS sockets if they are not open yet.
func (d *Discovery) ensureTransport() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transportLocked()
}

// transportLocked opens the mDNS sockets if they are not open yet. The caller
// must hold d.mu.
func (d *Discovery) transportLocked() error {
	if d.stopped {
		return ErrStopped
	}
	if d.tr != nil {
		return nil
	}

	ifaces := d.interfaces()
	if len(ifaces) == 0 {
		return ErrNoInterfaces
	}
	tr, err := openTransport(ifaces, d.log, d.access)
	if err != nil {
		return err
	}

	d.tr = tr
	d.ifaceHash = ifaceHash(ifaces)
	d.epoch = Epoch(d.clock.Now())
	d.wg.Add(1)
	go d.dispatch(tr)
	return nil
}

// interfaces returns the interfaces used for discovery.
func (d *Discovery) interfaces() []net.Interface {
	if len(d.opts.Ifaces) > 0 {
		return cloneIfaces(d.opts.Ifaces)
	}

	all, err := net.Interfaces()
	if err != nil {
		debugf(d.log, "list network interfaces: %v", err)
		return nil
	}
	var ifaces []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		ifaces = append(ifaces, iface)
	}
	return ifaces
}

// randomLabel returns a random lowercase base32 label.
func randomLabel() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("discovery: random label: %w", err)
	}
	return encodeLabel(buf[:]), nil
}

// ifaceHash returns a value that changes with the addresses of the interfaces.
func ifaceHash(ifaces []net.Interface) string {
	var b strings.Builder
	for _, iface := range ifaces {
		b.WriteString(strconv.Itoa(iface.Index))
		b.WriteByte(':')
		b.WriteString(iface.Name)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(int(iface.Flags)))
		b.WriteByte(':')
		addrs, err := iface.Addrs()
		if err != nil {
			b.WriteString(err.Error())
		}
		for _, addr := range addrs {
			b.WriteString(addr.String())
			b.WriteByte(',')
		}
		b.WriteByte('|')
	}
	return b.String()
}

// cloneIfaces copies ifaces.
func cloneIfaces(ifaces []net.Interface) []net.Interface {
	if len(ifaces) == 0 {
		return nil
	}
	return append([]net.Interface(nil), ifaces...)
}
