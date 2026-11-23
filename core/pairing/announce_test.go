package pairing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
)

// recordingDiscovery records the discovery calls of a window.
type recordingDiscovery struct {
	mu      sync.Mutex
	adverts []advertised
	browses []ports.Service
	stops   []ports.Service
	browsed chan struct{}
	once    sync.Once
}

// advertised is one announcement of the recording discovery.
type advertised struct {
	service ports.Service
	port    int
}

// newRecordingDiscovery creates a recording discovery.
func newRecordingDiscovery() *recordingDiscovery {
	return &recordingDiscovery{browsed: make(chan struct{})}
}

// Advertise records the announcement.
func (d *recordingDiscovery) Advertise(ctx context.Context, service ports.Service, port int, _ map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.adverts = append(d.adverts, advertised{service: service, port: port})
	return nil
}

// Browse records the browse and returns an empty stream.
func (d *recordingDiscovery) Browse(ctx context.Context, service ports.Service) (<-chan ports.Candidate, error) {
	d.mu.Lock()
	d.browses = append(d.browses, service)
	d.mu.Unlock()
	d.once.Do(func() { close(d.browsed) })

	candidates := make(chan ports.Candidate)
	go func() {
		<-ctx.Done()
		close(candidates)
	}()
	return candidates, nil
}

// StopService records the withdrawal.
func (d *recordingDiscovery) StopService(service ports.Service) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stops = append(d.stops, service)
	return nil
}

// announcements returns the recorded announcements.
func (d *recordingDiscovery) announcements() []advertised {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]advertised(nil), d.adverts...)
}

// browsedServices returns the recorded browse calls.
func (d *recordingDiscovery) browsedServices() []ports.Service {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ports.Service(nil), d.browses...)
}

// withdrawn returns the recorded withdrawals.
func (d *recordingDiscovery) withdrawn() []ports.Service {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ports.Service(nil), d.stops...)
}

// TestWindowAnnouncesPairingService checks that the window announces the
// pairing service on its listener port and withdraws it when it closes.
func TestWindowAnnouncesPairingService(t *testing.T) {
	network, _ := testNetwork(t)
	node := newNode(t, network, fake.NewBus(), 1, 41000)
	discovery := newRecordingDiscovery()

	window, err := Open(context.Background(), Options{
		Identity:     node.identity,
		Trust:        node.trust,
		Discovery:    discovery,
		Name:         node.name,
		Platform:     model.PlatformMacOS,
		Role:         model.RolePersonal,
		ListenPacket: node.sockets.ListenPacket,
	})
	if err != nil {
		t.Fatalf("open window: %v", err)
	}

	adverts := discovery.announcements()
	if len(adverts) != 1 || adverts[0].service != ports.ServicePairing {
		t.Fatalf("advertised %v, want the pairing service", adverts)
	}
	if got, want := adverts[0].port, window.Port(); got != want {
		t.Errorf("advertised port %d, want the listener port %d", got, want)
	}
	if window.Addr() == nil || window.Port() == 0 {
		t.Fatalf("listener address = %v", window.Addr())
	}

	select {
	case <-discovery.browsed:
	case <-time.After(testTimeout):
		t.Fatal("the window does not browse the pairing service")
	}
	if got := discovery.browsedServices(); len(got) != 1 || got[0] != ports.ServicePairing {
		t.Errorf("browsed %v, want the pairing service", got)
	}

	if err := window.Close(); err != nil {
		t.Fatalf("close window: %v", err)
	}
	if got := discovery.withdrawn(); len(got) != 1 || got[0] != ports.ServicePairing {
		t.Fatalf("withdrew %v, want the pairing service", got)
	}
}
