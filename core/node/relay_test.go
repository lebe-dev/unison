package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/relay"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	relayserver "github.com/lebe-dev/unison/internal/relay"
)

// relayTestToken is the access token of the relay of the tests.
const relayTestToken = "test-token"

// newRelayNode creates a node on the virtual network with short connection
// timeouts, so a blocked local network is noticed quickly.
func newRelayNode(t *testing.T, network *vnet.Network, bus *fake.Bus, base int, cfg Config) *testNode {
	t.Helper()

	dir := t.TempDir()
	clipboard := fake.NewClipboard()
	sockets := newSocketFactory(network, base)
	cfg.DataDir = dir
	cfg.Name = fmt.Sprintf("device-%d", base)
	cfg.Platform = model.PlatformMacOS
	cfg.Keystore = fake.NewKeystore()
	cfg.Clipboard = clipboard
	cfg.Discovery = bus.Discovery()
	cfg.Logger = testLogger(base)
	cfg.ShareDownloads = filepath.Join(dir, "Downloads")
	cfg.ListenPacket = sockets.listenPacket
	cfg.Limits = Limits{
		DialTimeout:       300 * time.Millisecond,
		ReconnectDelay:    50 * time.Millisecond,
		ReconnectMaxDelay: 500 * time.Millisecond,
		RelayDelay:        300 * time.Millisecond,
		RelayGrace:        500 * time.Millisecond,
	}

	created, err := New(cfg)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	n := &testNode{Node: created, clipboard: clipboard, sockets: sockets}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := n.Stop(ctx); err != nil {
			t.Errorf("stop node: %v", err)
		}
	})
	return n
}

// setRelay stores the relay access of the group.
func setRelay(t *testing.T, n *testNode, url string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := n.SetRelay(ctx, trust.RelaySettings{URL: url, Token: relayTestToken}); err != nil {
		t.Fatalf("set relay: %v", err)
	}
}

// waitPath waits until the node has a session with the peer over the path.
func waitPath(t *testing.T, n *testNode, peer model.DeviceID, path model.Path) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if state := n.peerState(peer); state.Online && state.Path == path {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s connection with %s within %v (%s)", path, peer, testTimeout, n.linkState(peer))
}

// settle waits until neither node replaces its session for a moment, so an
// announcement is not sent into a connection that is about to go away.
func settle(t *testing.T, a, b *testNode) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	var state string
	since := time.Now()
	for time.Now().Before(deadline) {
		current := fmt.Sprintf("%d/%d", a.generation(b.DeviceID()), b.generation(a.DeviceID()))
		if current != state {
			state = current
			since = time.Now()
		}
		if current != "0/0" && time.Since(since) >= 300*time.Millisecond {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the sessions did not settle within %v (generations %s)", testTimeout, state)
}

// submitText puts text into the local clipboard, which announces it to the
// peers.
func submitText(t *testing.T, n *testNode, text string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := n.Clip().Submit(ctx, ports.Content{
		Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
	}); err != nil {
		t.Fatalf("submit text: %v", err)
	}
}

func TestRelayPathSyncsText(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	server := startNodeTestRelay(t)

	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}
	a := newRelayNode(t, network, bus, 21000, Config{Relay: trusted})
	b := newRelayNode(t, network, bus, 21004, Config{Relay: trusted})

	a.start(t)
	b.start(t)

	// The two devices are in different networks: the local one carries nothing.
	a.sockets.disconnect()
	b.sockets.disconnect()

	setRelay(t, a, server.url)
	setRelay(t, b, server.url)
	a.pair(t, b)
	b.pair(t, a)

	waitPath(t, a, b.DeviceID(), model.PathRelay)
	waitPath(t, b, a.DeviceID(), model.PathRelay)
	settle(t, a, b)

	submitText(t, a, "hello through the relay")
	waitClipboard(t, b.clipboard, "hello through the relay")

	// The local network appears: both devices move the session to it.
	a.sockets.reconnect()
	b.sockets.reconnect()

	waitPath(t, a, b.DeviceID(), model.PathLAN)
	waitPath(t, b, a.DeviceID(), model.PathLAN)
	settle(t, a, b)

	submitText(t, b, "hello from the local network")
	waitClipboard(t, a.clipboard, "hello from the local network")
}

func TestRelayPathFallsBackToWebSocket(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	server := startNodeTestRelay(t)

	// A socket that swallows the datagrams simulates a network that blocks UDP.
	blackhole, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind blackhole: %v", err)
	}
	t.Cleanup(func() { _ = blackhole.Close() })

	blocked := relay.Config{
		TLS:      &tls.Config{InsecureSkipVerify: true},
		QUICAddr: blackhole.LocalAddr().String(),
	}
	direct := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}

	a := newRelayNode(t, network, bus, 21008, Config{Relay: blocked})
	b := newRelayNode(t, network, bus, 21012, Config{Relay: direct})

	a.start(t)
	b.start(t)
	a.sockets.disconnect()
	b.sockets.disconnect()

	setRelay(t, a, server.url)
	setRelay(t, b, server.url)
	a.pair(t, b)
	b.pair(t, a)

	waitPath(t, a, b.DeviceID(), model.PathRelay)
	waitPath(t, b, a.DeviceID(), model.PathRelay)
	settle(t, a, b)

	if status := a.RelayStatus(); status.Transport != relay.TransportWebSocket {
		t.Fatalf("the device did not fall back to WebSocket: %+v", status)
	}
	if status := b.RelayStatus(); status.Transport != relay.TransportQUIC {
		t.Fatalf("unexpected transport of the direct device: %+v", status)
	}

	submitText(t, a, "hello from a blocked network")
	waitClipboard(t, b.clipboard, "hello from a blocked network")
}

// nodeTestRelay is an in-process relay server on one port for both transports.
type nodeTestRelay struct {
	url      string
	certFile string
	keyFile  string
	server   *relayserver.Server
}

// startNodeTestRelay runs a relay server that the nodes of the tests use.
func startNodeTestRelay(t *testing.T) *nodeTestRelay {
	t.Helper()

	certFile, keyFile := nodeTestCertificate(t)
	address := fmt.Sprintf("127.0.0.1:%d", nodeTestPort(t))
	relay := &nodeTestRelay{url: address, certFile: certFile, keyFile: keyFile}
	relay.start(t)
	t.Cleanup(func() {
		if relay.server != nil {
			if err := relay.server.Close(); err != nil {
				t.Errorf("close relay: %v", err)
			}
		}
	})
	return relay
}

// start binds the relay server.
func (r *nodeTestRelay) start(t *testing.T) {
	t.Helper()

	cfg := relayserver.DefaultConfig()
	cfg.QUICAddr = r.url
	cfg.WSAddr = r.url
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.TLSCertFile = r.certFile
	cfg.TLSKeyFile = r.keyFile
	cfg.Tokens = []string{relayTestToken}
	cfg.MaxDatagramBytes = 1400
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	server, err := relayserver.New(cfg)
	if err != nil {
		t.Fatalf("new relay: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	r.server = server
}

// stop closes the relay server. The address stays usable for a restart.
func (r *nodeTestRelay) stop(t *testing.T) {
	t.Helper()

	if r.server == nil {
		return
	}
	if err := r.server.Close(); err != nil {
		t.Fatalf("close relay: %v", err)
	}
	r.server = nil
}

// nodeTestPort returns a port that is free for TCP and UDP on the loopback
// address.
func nodeTestPort(t *testing.T) int {
	t.Helper()

	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen TCP: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()

		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		_ = pc.Close()
		return port
	}
	t.Fatalf("no free port for both transports")
	return 0
}

// nodeTestCertificate writes a self-signed certificate for the loopback
// address.
func nodeTestCertificate(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: randSerial(t),
		Subject:      pkix.Name{CommonName: "relay test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

// randSerial returns a certificate serial number.
func randSerial(t *testing.T) *big.Int {
	t.Helper()

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	return serial
}

func TestRelaySettingsReachTheGroup(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	server := startNodeTestRelay(t)

	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}
	a := newRelayNode(t, network, bus, 21024, Config{Relay: trusted})
	b := newRelayNode(t, network, bus, 21028, Config{Relay: trusted})

	a.start(t)
	b.start(t)
	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	settings := trust.RelaySettings{URL: server.url, Token: relayTestToken}
	if err := a.SetRelay(ctx, settings); err != nil {
		t.Fatalf("set relay: %v", err)
	}

	// The group data carries the access to the other device.
	waitGroupRelay(t, b, settings)

	// The devices leave the local network, so both use the relay they share.
	a.lookup(b.DeviceID()).close()
	b.lookup(a.DeviceID()).close()
	a.sockets.disconnect()
	b.sockets.disconnect()

	waitPath(t, a, b.DeviceID(), model.PathRelay)
	waitPath(t, b, a.DeviceID(), model.PathRelay)
	settle(t, a, b)

	for _, n := range []*testNode{a, b} {
		if status := n.RelayStatus(); !status.Connected || status.Transport != relay.TransportQUIC {
			t.Fatalf("%s is not connected over QUIC: %+v", n.DeviceID(), status)
		}
	}
	submitText(t, b, "hello over the shared relay")
	waitClipboard(t, a.clipboard, "hello over the shared relay")

	// The local network comes back, so both devices move to it.
	a.sockets.reconnect()
	b.sockets.reconnect()
	waitPath(t, a, b.DeviceID(), model.PathLAN)
	waitPath(t, b, a.DeviceID(), model.PathLAN)
	settle(t, a, b)

	// Turning the relay off on one device turns it off for the group.
	if err := a.SetRelay(ctx, trust.RelaySettings{}); err != nil {
		t.Fatalf("turn the relay off: %v", err)
	}
	waitGroupRelay(t, b, trust.RelaySettings{})
	waitRelayOff(t, a)
	waitRelayOff(t, b)
}

// waitRelayOff waits until the relay client of the node is not connected.
func waitRelayOff(t *testing.T, n *testNode) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if !n.RelayStatus().Connected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the relay client of %s is still connected", n.DeviceID())
}

// waitGroupRelay waits until the group data of the node carries the relay
// settings.
func waitGroupRelay(t *testing.T, n *testNode, want trust.RelaySettings) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	var data trust.GroupData
	for time.Now().Before(deadline) {
		read, err := n.Trust().GroupData(context.Background())
		if err == nil {
			data = read
			if data.Relay == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the group relay of %s is %+v within %v, want %+v", n.DeviceID(), data.Relay, testTimeout, want)
}

func TestRelayPathReconnects(t *testing.T) {
	network := vnet.NewNetwork()
	bus := fake.NewBus()
	server := startNodeTestRelay(t)

	trusted := relay.Config{TLS: &tls.Config{InsecureSkipVerify: true}}
	a := newRelayNode(t, network, bus, 21016, Config{Relay: trusted})
	b := newRelayNode(t, network, bus, 21020, Config{Relay: trusted})

	a.start(t)
	b.start(t)
	a.sockets.disconnect()
	b.sockets.disconnect()

	setRelay(t, a, server.url)
	setRelay(t, b, server.url)
	a.pair(t, b)
	b.pair(t, a)

	waitPath(t, a, b.DeviceID(), model.PathRelay)
	waitPath(t, b, a.DeviceID(), model.PathRelay)
	settle(t, a, b)

	// The relay goes away and comes back on the same address.
	server.stop(t)
	waitOffline(t, a, b.DeviceID())
	waitOffline(t, b, a.DeviceID())
	server.start(t)

	waitPath(t, a, b.DeviceID(), model.PathRelay)
	waitPath(t, b, a.DeviceID(), model.PathRelay)
	settle(t, a, b)

	submitText(t, b, "hello after the reconnect")
	waitClipboard(t, a.clipboard, "hello after the reconnect")
}
