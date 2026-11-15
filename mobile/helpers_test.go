package mobile

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/transport/vnet"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// testTimeout bounds a wait in the tests.
const testTimeout = 20 * time.Second

// fakePlatform records the callbacks of the core. The keystore is a reversible
// transformation that marks the wrapped blob.
type fakePlatform struct {
	events    chan []byte
	clips     chan []byte
	announces chan []byte
}

// newFakePlatform creates a platform with buffered callback queues.
func newFakePlatform() *fakePlatform {
	return &fakePlatform{
		events:    make(chan []byte, 64),
		clips:     make(chan []byte, 8),
		announces: make(chan []byte, 8),
	}
}

// OnEvent queues an event.
func (p *fakePlatform) OnEvent(evt []byte) {
	select {
	case p.events <- evt:
	default:
	}
}

// WriteClipboard queues clipboard content.
func (p *fakePlatform) WriteClipboard(item []byte) {
	p.clips <- item
}

// OpenBlobSink reports that no sink is available.
func (p *fakePlatform) OpenBlobSink([]byte) int {
	return -1
}

// KeystoreUnwrap reverses KeystoreWrap.
func (p *fakePlatform) KeystoreUnwrap(blob []byte) []byte {
	const prefix = "wrapped:"
	if len(blob) < len(prefix) || string(blob[:len(prefix)]) != prefix {
		return nil
	}
	return append([]byte(nil), blob[len(prefix):]...)
}

// KeystoreWrap marks the plaintext as wrapped.
func (p *fakePlatform) KeystoreWrap(plain []byte) []byte {
	return append([]byte("wrapped:"), plain...)
}

// Announce queues a service announcement.
func (p *fakePlatform) Announce(info []byte) {
	p.announces <- info
}

// nextAnnouncement returns the next announcement.
func (p *fakePlatform) nextAnnouncement(t *testing.T) *mobilev1.Announcement {
	t.Helper()
	select {
	case data := <-p.announces:
		msg := &mobilev1.Announcement{}
		if err := proto.Unmarshal(data, msg); err != nil {
			t.Fatalf("decode announcement: %v", err)
		}
		return msg
	case <-time.After(testTimeout):
		t.Fatal("no announcement")
		return nil
	}
}

// waitEvent returns the next event of the type.
func (p *fakePlatform) waitEvent(t *testing.T, want mobilev1.EventType) *mobilev1.Event {
	t.Helper()
	timeout := time.After(testTimeout)
	for {
		select {
		case data := <-p.events:
			event := &mobilev1.Event{}
			if err := proto.Unmarshal(data, event); err != nil {
				t.Fatalf("decode event: %v", err)
			}
			if event.GetType() == want {
				return event
			}
		case <-timeout:
			t.Fatalf("no event of type %s", want)
			return nil
		}
	}
}

// socketFactory binds virtual packet connections on consecutive addresses.
type socketFactory struct {
	network *vnet.Network
	next    int

	mu    sync.Mutex
	addrs []string
}

// newSocketFactory returns a factory that starts at base.
func newSocketFactory(network *vnet.Network, base int) *socketFactory {
	return &socketFactory{network: network, next: base}
}

// listenPacket binds the next virtual address.
func (s *socketFactory) listenPacket(_ context.Context, _ int) (net.PacketConn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.next))
	s.next++
	pc, err := s.network.ListenPacket(addr)
	if err != nil {
		return nil, err
	}
	s.addrs = append(s.addrs, addr)
	return pc, nil
}

// bound returns the address bound by the call with the index.
func (s *socketFactory) bound(index int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.addrs) {
		return ""
	}
	return s.addrs[index]
}

// testConfig returns a facade configuration on the virtual network.
func testConfig(t *testing.T, sockets *socketFactory) config {
	t.Helper()
	return config{
		dataDir: t.TempDir(),
		name:    "phone",
		listen:  sockets.listenPacket,
		limits:  fastLimits(),
		logger:  discardLogger(),
	}
}

// fastLimits returns short connection timings.
func fastLimits() node.Limits {
	return node.Limits{
		DialTimeout:       3 * time.Second,
		ReconnectDelay:    50 * time.Millisecond,
		ReconnectMaxDelay: 500 * time.Millisecond,
	}
}

// discardLogger returns a logger that writes nothing.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
