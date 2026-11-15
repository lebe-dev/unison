package mobile

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	corediscovery "github.com/lebe-dev/unison/core/discovery"
	"github.com/lebe-dev/unison/core/ports"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// Discovery limits of the facade.
const (
	// defaultRefresh is how often the tag of an announcement is checked.
	defaultRefresh = 30 * time.Second
	// candidateBuffer is the queue of one browse channel.
	candidateBuffer = 64
	// maxAddrLength bounds one candidate address.
	maxAddrLength = 255
	// txtVersion is the TXT key of the protocol version.
	txtVersion = "v"
	// txtTag is the TXT key of the discovery tag.
	txtTag = "tag"
)

// instanceEncoding encodes the instance name of a service.
var instanceEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// discoveryBridge announces services through the platform and receives the
// candidates the platform discovered.
type discoveryBridge struct {
	platform Platform
	clock    ports.Clock
	log      *slog.Logger
	refresh  time.Duration
	key      func(context.Context) ([]byte, error)

	mu       sync.Mutex
	ads      map[ports.Service]*advertisement
	browsers map[*browser]struct{}
	stopped  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// advertisement is one announced service instance.
type advertisement struct {
	service  ports.Service
	instance string
	port     int
	base     map[string]string
	txt      map[string]string
	cancel   context.CancelFunc
}

// browser is one open browse channel.
type browser struct {
	service ports.Service
	ch      chan ports.Candidate
}

var _ ports.Discovery = (*discoveryBridge)(nil)

// newDiscoveryBridge creates a discovery bridge without a key provider.
func newDiscoveryBridge(platform Platform, clock ports.Clock, log *slog.Logger) *discoveryBridge {
	return &discoveryBridge{
		platform: platform,
		clock:    clock,
		log:      log,
		refresh:  defaultRefresh,
		ads:      make(map[ports.Service]*advertisement),
		browsers: make(map[*browser]struct{}),
		stopCh:   make(chan struct{}),
	}
}

// Advertise asks the platform to announce the service.
func (d *discoveryBridge) Advertise(ctx context.Context, service ports.Service, port int, txt map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkService(service); err != nil {
		return err
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("mobile: discovery port %d is invalid", port)
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return errors.New("mobile: discovery is stopped")
	}
	ad, ok := d.ads[service]
	if !ok {
		instance, err := newInstance()
		if err != nil {
			d.mu.Unlock()
			return err
		}
		adCtx, cancel := context.WithCancel(context.Background())
		ad = &advertisement{service: service, instance: instance, cancel: cancel}
		d.ads[service] = ad
		d.wg.Add(1)
		go d.refreshLoop(adCtx, ad)
	}
	ad.port = port
	ad.base = maps.Clone(txt)
	d.mu.Unlock()

	records := d.records(ctx, service, txt)
	d.mu.Lock()
	if d.stopped || d.ads[service] != ad {
		d.mu.Unlock()
		return nil
	}
	ad.txt = records
	instance := ad.instance
	d.mu.Unlock()

	d.announce(service, instance, port, records, false)
	return nil
}

// StopService withdraws the announcement of the service.
func (d *discoveryBridge) StopService(service ports.Service) error {
	if err := checkService(service); err != nil {
		return err
	}

	d.mu.Lock()
	ad, ok := d.ads[service]
	var instance string
	var port int
	if ok {
		delete(d.ads, service)
		ad.cancel()
		instance, port = ad.instance, ad.port
	}
	d.mu.Unlock()
	if !ok {
		return nil
	}
	d.announce(service, instance, port, nil, true)
	return nil
}

// Browse delivers the candidates the host reported for the service.
func (d *discoveryBridge) Browse(ctx context.Context, service ports.Service) (<-chan ports.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkService(service); err != nil {
		return nil, err
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return nil, errors.New("mobile: discovery is stopped")
	}
	b := &browser{service: service, ch: make(chan ports.Candidate, candidateBuffer)}
	d.browsers[b] = struct{}{}
	d.wg.Add(1)
	d.mu.Unlock()

	go func() {
		defer d.wg.Done()
		select {
		case <-ctx.Done():
		case <-d.stopCh:
		}
		d.mu.Lock()
		delete(d.browsers, b)
		close(b.ch)
		d.mu.Unlock()
	}()
	return b.ch, nil
}

// Stop withdraws every announcement and closes every browse channel.
func (d *discoveryBridge) Stop() error {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return nil
	}
	d.stopped = true
	close(d.stopCh)
	type withdrawal struct {
		service  ports.Service
		instance string
		port     int
	}
	ads := make([]*advertisement, 0, len(d.ads))
	withdrawals := make([]withdrawal, 0, len(d.ads))
	for _, ad := range d.ads {
		ads = append(ads, ad)
		withdrawals = append(withdrawals, withdrawal{service: ad.service, instance: ad.instance, port: ad.port})
	}
	clear(d.ads)
	d.mu.Unlock()

	for _, ad := range ads {
		ad.cancel()
	}
	for _, withdrawal := range withdrawals {
		d.announce(withdrawal.service, withdrawal.instance, withdrawal.port, nil, true)
	}
	d.wg.Wait()
	return nil
}

// Add delivers a candidate reported by the host.
func (d *discoveryBridge) Add(service string, addr string, rawTXT []byte) {
	target := ports.Service(service)
	if service == "" {
		target = ports.ServiceMain
	}
	if checkService(target) != nil {
		d.log.Debug("ignored a candidate of an unknown service", "service", service)
		return
	}
	if addr == "" || len(addr) > maxAddrLength {
		d.log.Debug("ignored a candidate with an invalid address")
		return
	}

	txt := make(map[string]string)
	if len(rawTXT) > 0 {
		msg := &mobilev1.CandidateTxt{}
		if err := proto.Unmarshal(rawTXT, msg); err != nil {
			d.log.Debug("ignored a candidate with invalid metadata", "error", err)
			return
		}
		txt = msg.GetRecords()
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	for b := range d.browsers {
		if b.service != target {
			continue
		}
		select {
		case b.ch <- ports.Candidate{Addr: addr, TXT: maps.Clone(txt)}:
		default:
			d.log.Debug("dropped a candidate, the browse queue is full")
		}
	}
}

// refreshLoop re-announces the service when its tag changes.
func (d *discoveryBridge) refreshLoop(ctx context.Context, ad *advertisement) {
	defer d.wg.Done()

	interval := d.refresh
	if interval <= 0 {
		interval = defaultRefresh
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.refreshAdvert(ctx, ad)
		}
	}
}

// refreshAdvert publishes the announcement again when the tag moved.
func (d *discoveryBridge) refreshAdvert(ctx context.Context, ad *advertisement) {
	d.mu.Lock()
	if d.stopped || d.ads[ad.service] != ad {
		d.mu.Unlock()
		return
	}
	service, base, last := ad.service, maps.Clone(ad.base), maps.Clone(ad.txt)
	d.mu.Unlock()

	records := d.records(ctx, service, base)
	if maps.Equal(records, last) {
		return
	}

	d.mu.Lock()
	if d.stopped || d.ads[service] != ad {
		d.mu.Unlock()
		return
	}
	ad.txt = records
	instance, port := ad.instance, ad.port
	d.mu.Unlock()

	d.announce(service, instance, port, records, false)
}

// records builds the TXT records of an announcement.
func (d *discoveryBridge) records(ctx context.Context, service ports.Service, base map[string]string) map[string]string {
	records := make(map[string]string, len(base)+2)
	for key, value := range base {
		records[key] = value
	}
	records[txtVersion] = "1"
	if service != ports.ServiceMain {
		return records
	}
	if tag := d.tag(ctx); tag != "" {
		records[txtTag] = tag
	}
	return records
}

// tag returns the discovery tag of this epoch, or an empty string without a
// key.
func (d *discoveryBridge) tag(ctx context.Context) string {
	if d.key == nil {
		return ""
	}
	key, err := d.key(ctx)
	if err != nil {
		d.log.Debug("discovery key is unavailable", "error", err)
		return ""
	}
	return corediscovery.Tag(key, d.clock.Now())
}

// announce hands one announcement to the platform.
func (d *discoveryBridge) announce(service ports.Service, instance string, port int, txt map[string]string, withdraw bool) {
	msg := &mobilev1.Announcement{
		Service:  string(service),
		Withdraw: withdraw,
		Instance: instance,
		Port:     int32(port),
		Txt:      txt,
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		d.log.Warn("encoding a discovery announcement failed", "error", err)
		return
	}

	d.call(data)
}

// call invokes the platform announcement callback.
func (d *discoveryBridge) call(data []byte) {
	defer func() {
		if recover() != nil {
			d.log.Warn("the platform announcement callback panicked")
		}
	}()
	d.platform.Announce(data)
}

// checkService reports whether the service can be announced or browsed.
func checkService(service ports.Service) error {
	switch service {
	case ports.ServiceMain, ports.ServicePairing:
		return nil
	default:
		return fmt.Errorf("mobile: unknown discovery service %q", service)
	}
}

// newInstance creates a random instance name.
func newInstance() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mobile: create instance name: %w", err)
	}
	return strings.ToLower(instanceEncoding.EncodeToString(raw)), nil
}
