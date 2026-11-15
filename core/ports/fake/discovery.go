package fake

import (
	"context"
	"maps"
	"net"
	"strconv"
	"sync"

	"github.com/lebe-dev/unison/core/ports"
)

// candidateBuffer is the number of candidates buffered per browser.
const candidateBuffer = 16

// Bus connects fake discovery instances so several nodes can find each other in one process.
type Bus struct {
	mu    sync.Mutex
	nodes []*Discovery
}

// NewBus creates an empty discovery bus.
func NewBus() *Bus {
	return &Bus{}
}

// Discovery creates a ports.Discovery attached to the bus.
func (b *Bus) Discovery() *Discovery {
	b.mu.Lock()
	defer b.mu.Unlock()
	d := &Discovery{
		bus:      b,
		ads:      make(map[ports.Service]advert),
		watchers: make(map[int]*watch),
	}
	b.nodes = append(b.nodes, d)
	return d
}

type advert struct {
	addr string
	txt  map[string]string
}

type watch struct {
	service ports.Service
	ch      chan ports.Candidate
}

// Discovery is a ports.Discovery backed by a Bus.
type Discovery struct {
	bus *Bus
	// ads and watchers are guarded by bus.mu.
	ads      map[ports.Service]advert
	watchers map[int]*watch
	next     int
}

var _ ports.Discovery = (*Discovery)(nil)

// Advertise announces a service instance on the bus.
func (d *Discovery) Advertise(ctx context.Context, service ports.Service, port int, txt map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ad := advert{
		addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		txt:  maps.Clone(txt),
	}

	b := d.bus
	b.mu.Lock()
	defer b.mu.Unlock()
	d.ads[service] = ad
	for _, node := range b.nodes {
		if node == d {
			continue
		}
		for _, w := range node.watchers {
			if w.service != service {
				continue
			}
			deliver(w, ad)
		}
	}
	return nil
}

// Browse delivers the instances advertised by the other nodes of the bus.
func (d *Discovery) Browse(ctx context.Context, service ports.Service) (<-chan ports.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := d.bus
	b.mu.Lock()
	id := d.next
	d.next++
	w := &watch{service: service, ch: make(chan ports.Candidate, candidateBuffer)}
	d.watchers[id] = w
	for _, node := range b.nodes {
		if node == d {
			continue
		}
		if ad, ok := node.ads[service]; ok {
			deliver(w, ad)
		}
	}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		d.closeWatch(id)
	}()
	return w.ch, nil
}

// Stop removes every advertisement and closes every browse channel.
// The instance stays usable.
func (d *Discovery) Stop() error {
	d.bus.mu.Lock()
	defer d.bus.mu.Unlock()
	clear(d.ads)
	for id := range d.watchers {
		d.closeWatch(id)
	}
	return nil
}

// closeWatch closes a browse channel. The caller must hold bus.mu.
func (d *Discovery) closeWatch(id int) {
	w, ok := d.watchers[id]
	if !ok {
		return
	}
	delete(d.watchers, id)
	close(w.ch)
}

func deliver(w *watch, ad advert) {
	select {
	case w.ch <- ports.Candidate{Addr: ad.addr, TXT: maps.Clone(ad.txt)}:
	default:
	}
}
