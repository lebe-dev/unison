package pairing

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// scanConfig is the transport setting of the tests that wait for a refusal.
func scanConfig() transport.Config {
	return transport.Config{HandshakeTimeout: time.Second}
}

// scanResult is what the scanning device reports.
type scanResult struct {
	err    error
	events []Event
}

// testScan runs Scan with the given code and returns its events.
func testScan(t *testing.T, scanner *testNode, code QR) <-chan scanResult {
	t.Helper()

	uri, err := code.URI()
	if err != nil {
		t.Fatalf("build the code: %v", err)
	}
	results := make(chan scanResult, 1)
	events := make(chan Event, 8)
	go func() {
		err := Scan(context.Background(), Options{
			Identity:     scanner.identity,
			Trust:        scanner.trust,
			Name:         scanner.name,
			Platform:     model.PlatformMacOS,
			ListenPacket: scanner.sockets.ListenPacket,
			Config:       scanConfig(),
		}, uri, func(event Event) {
			events <- event
		})
		close(events)
		collected := make([]Event, 0, 8)
		for event := range events {
			collected = append(collected, event)
		}
		results <- scanResult{err: err, events: collected}
	}()
	return results
}

// waitScan waits for the end of a scan.
func waitScan(t *testing.T, results <-chan scanResult) scanResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(testTimeout):
		t.Fatal("the scan did not finish")
		return scanResult{}
	}
}

// TestScanPairsTwoDevices checks the successful QR flow: the scanning device
// connects with the pinned certificate, the showing device confirms and both
// devices write the peer.
func TestScanPairsTwoDevices(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	if len(code.Addrs) != 1 || code.Addrs[0] != showing.window.Addr().String() {
		t.Fatalf("addresses = %v, want %s", code.Addrs, showing.window.Addr())
	}
	if code.DeviceID != showing.deviceID() {
		t.Errorf("code device = %s, want %s", code.DeviceID, showing.deviceID())
	}
	if code.Fingerprint != showing.identity.CertFingerprint() {
		t.Errorf("code fingerprint = %s, want %s", code.Fingerprint, showing.identity.CertFingerprint())
	}
	if code.Role != model.RolePersonal {
		t.Errorf("code role = %s, want personal", code.Role)
	}

	results := testScan(t, scanner, code)
	request := waitEvent(t, showing.window, EventRequest)
	if request.Method != MethodQR {
		t.Errorf("request method = %s, want qr", request.Method)
	}
	if request.DeviceID != scanner.deviceID() {
		t.Errorf("request device = %s, want %s", request.DeviceID, scanner.deviceID())
	}
	if request.Name != scanner.name {
		t.Errorf("request name = %q, want %q", request.Name, scanner.name)
	}
	if request.Role != model.RolePersonal {
		t.Errorf("request role = %s, want personal", request.Role)
	}

	ctx := context.Background()
	if err := showing.window.Confirm(ctx, scanner.deviceID()); err != nil {
		t.Fatalf("confirm the request: %v", err)
	}
	result := waitScan(t, results)
	if result.err != nil {
		t.Fatalf("scan: %v", result.err)
	}
	if len(result.events) != 2 || result.events[0].Kind != EventRequest || result.events[1].Kind != EventPaired {
		t.Fatalf("scan events = %+v", result.events)
	}
	for _, event := range result.events {
		if event.Method != MethodQR {
			t.Errorf("scan event %s has method %s, want qr", event.Kind, event.Method)
		}
	}

	paired := waitEvent(t, showing.window, EventPaired)
	if paired.DeviceID != scanner.deviceID() {
		t.Errorf("paired device = %s, want %s", paired.DeviceID, scanner.deviceID())
	}

	assertPeer(t, showing.trust, scanner, model.RolePersonal)
	assertPeer(t, scanner.trust, showing, model.RolePersonal)
	assertGroupKeys(t, scanner, showing)

	// The token of a completed pairing is spent and replaced.
	next, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	if next.Token == code.Token {
		t.Fatal("the window kept the used token")
	}
}

// TestScanFriends checks a QR pair that proposes the Friends role: no group
// key is shared.
func TestScanFriends(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RoleFriends)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	results := testScan(t, scanner, code)
	request := waitEvent(t, showing.window, EventRequest)
	if request.Role != model.RoleFriends {
		t.Fatalf("request role = %s, want friends", request.Role)
	}
	if err := showing.window.Confirm(context.Background(), scanner.deviceID()); err != nil {
		t.Fatalf("confirm the request: %v", err)
	}
	if result := waitScan(t, results); result.err != nil {
		t.Fatalf("scan: %v", result.err)
	}

	assertPeer(t, showing.trust, scanner, model.RoleFriends)
	assertPeer(t, scanner.trust, showing, model.RoleFriends)
	for _, node := range []*testNode{scanner, showing} {
		if key, err := node.trust.DiscoveryKey(context.Background()); !errors.Is(err, trust.ErrSecretNotFound) {
			t.Fatalf("%s has discovery key %x, want none", node.name, key)
		}
	}
}

// TestScanWrongFingerprint checks that a code with another fingerprint is
// refused before any pairing request reaches the window.
func TestScanWrongFingerprint(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	code.Fingerprint[0] ^= 0xff

	result := waitScan(t, testScan(t, scanner, code))
	if result.err == nil {
		t.Fatal("the scan of a code with another fingerprint succeeded")
	}
	if result.err != nil && !errors.Is(result.err, transport.ErrPairingRejected) {
		t.Errorf("scan error = %v, want %v", result.err, transport.ErrPairingRejected)
	}

	assertNoPeer(t, showing.trust, scanner.deviceID())
	assertNoPeer(t, scanner.trust, showing.deviceID())
}

// TestScanWrongDeviceID checks that a code that names another device than the one
// behind the pinned certificate is refused.
func TestScanWrongDeviceID(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	code.DeviceID[0] ^= 0xff

	result := waitScan(t, testScan(t, scanner, code))
	if !errors.Is(result.err, ErrInvalidPeer) {
		t.Fatalf("scan error = %v, want %v", result.err, ErrInvalidPeer)
	}
	assertNoPeer(t, showing.trust, scanner.deviceID())
	assertNoPeer(t, scanner.trust, showing.deviceID())
}

// TestScanReusedToken checks that the token of a completed pairing cannot be
// used again while the window is open.
func TestScanReusedToken(t *testing.T) {
	network, bus := testNetwork(t)
	first := newNode(t, network, bus, 1, 41000)
	second := newNode(t, network, bus, 2, 42000)
	showing := newNode(t, network, bus, 3, 43000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}

	results := testScan(t, first, code)
	waitEvent(t, showing.window, EventRequest)
	if err := showing.window.Confirm(context.Background(), first.deviceID()); err != nil {
		t.Fatalf("confirm the request: %v", err)
	}
	if result := waitScan(t, results); result.err != nil {
		t.Fatalf("scan: %v", result.err)
	}
	waitEvent(t, showing.window, EventPaired)

	// The same code is refused, and no request reaches the window again.
	if result := waitScan(t, testScan(t, second, code)); result.err == nil {
		t.Fatal("a used token paired a second device")
	}
	failure := waitEvent(t, showing.window, EventFailed)
	if !errors.Is(failure.Err, ErrToken) {
		t.Errorf("failure = %v, want %v", failure.Err, ErrToken)
	}
	assertNoPeer(t, showing.trust, second.deviceID())
	assertNoPeer(t, second.trust, showing.deviceID())
}

// TestScanAfterWindowClosed checks that a code is useless once the window is
// closed: the listener is gone.
func TestScanAfterWindowClosed(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	if err := showing.window.Close(); err != nil {
		t.Fatalf("close the window: %v", err)
	}

	result := waitScan(t, testScan(t, scanner, code))
	if result.err == nil {
		t.Fatal("the scan of a closed window succeeded")
	}
	// The scanning device learns about the failure, so its screen stops waiting.
	if len(result.events) != 1 || result.events[0].Kind != EventFailed || result.events[0].Method != MethodQR {
		t.Fatalf("events of the scan = %v, want one QR failure", result.events)
	}
	if result.events[0].DeviceID != showing.deviceID() || !errors.Is(result.events[0].Err, ErrUnreachable) {
		t.Errorf("failure = %+v, want the device of the code and an error", result.events[0])
	}
	if _, err := showing.window.QR(); !errors.Is(err, ErrClosed) {
		t.Errorf("code of a closed window = %v, want %v", err, ErrClosed)
	}
	assertNoPeer(t, scanner.trust, showing.deviceID())
}

// TestScanRejected checks that a refusal on the showing device writes nothing on
// either device and gives the token back, so the same code can be scanned again.
func TestScanRejected(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}

	results := testScan(t, scanner, code)
	waitEvent(t, showing.window, EventRequest)
	if err := showing.window.Reject(context.Background(), scanner.deviceID()); err != nil {
		t.Fatalf("reject the request: %v", err)
	}
	result := waitScan(t, results)
	if !errors.Is(result.err, ErrRejected) {
		t.Fatalf("scan error = %v, want %v", result.err, ErrRejected)
	}
	failure := waitEvent(t, showing.window, EventFailed)
	if !errors.Is(failure.Err, ErrRejected) {
		t.Errorf("failure = %v, want %v", failure.Err, ErrRejected)
	}

	assertNoPeer(t, showing.trust, scanner.deviceID())
	assertNoPeer(t, scanner.trust, showing.deviceID())

	// A refused exchange does not spend the token.
	next, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	if next.Token != code.Token {
		t.Fatal("the refused exchange replaced the token")
	}
	if !slices.Equal(next.Addrs, code.Addrs) {
		t.Errorf("addresses = %v, want %v", next.Addrs, code.Addrs)
	}

	// The same code pairs on the second try.
	results = testScan(t, scanner, code)
	waitEvent(t, showing.window, EventRequest)
	if err := showing.window.Confirm(context.Background(), scanner.deviceID()); err != nil {
		t.Fatalf("confirm the request: %v", err)
	}
	if result := waitScan(t, results); result.err != nil {
		t.Fatalf("scan: %v", result.err)
	}
	assertPeer(t, showing.trust, scanner, model.RolePersonal)
	assertPeer(t, scanner.trust, showing, model.RolePersonal)
}

// TestScanRoleMismatch checks that a code of another role does not pair with a
// window that proposes a different one.
func TestScanRoleMismatch(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}
	code.Role = model.RoleFriends

	if result := waitScan(t, testScan(t, scanner, code)); result.err == nil {
		t.Fatal("the scan of a code with another role succeeded")
	}
	failure := waitEvent(t, showing.window, EventFailed)
	if !errors.Is(failure.Err, ErrRoleMismatch) {
		t.Errorf("failure = %v, want %v", failure.Err, ErrRoleMismatch)
	}
	assertNoPeer(t, showing.trust, scanner.deviceID())
}

// TestScanUnsignedRequest checks that the window refuses a request that carries
// a token without a signature.
func TestScanUnsignedRequest(t *testing.T) {
	network, bus := testNetwork(t)
	scanner := newNode(t, network, bus, 1, 41000)
	showing := newNode(t, network, bus, 2, 42000)
	showing.open(t, ports.SystemClock, model.RolePersonal)

	code, err := showing.window.QR()
	if err != nil {
		t.Fatalf("code of the window: %v", err)
	}

	pc, err := scanner.sockets.ListenPacket(context.Background())
	if err != nil {
		t.Fatalf("packet connection: %v", err)
	}
	dialer, err := transport.NewDialer(pc, scanner.identity, unpairedVerifier{}, scanConfig())
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	defer func() { _ = dialer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := dialer.DialPairing(ctx, vnet.NewAddr(showing.window.Addr().String()), code.Fingerprint)
	if err != nil {
		t.Fatalf("dial the window: %v", err)
	}
	defer func() { _ = conn.Close() }()

	stream, err := conn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("open the pairing stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	public := scanner.identity.Public()
	request := &wirev1.PairRequest{
		Token:      code.Token[:],
		Ed25519Pub: public.Ed25519,
		MldsaPub:   public.MLDSABytes(),
		DeviceName: scanner.name,
		Platform:   wirev1.Platform_PLATFORM_MACOS,
		Role:       wirev1.Role_ROLE_PERSONAL,
	}
	if err := writeMessage(stream, request); err != nil {
		t.Fatalf("send the request: %v", err)
	}
	// The request is delivered; the window refuses it without answering.
	_ = stream.Close()

	failure := waitEvent(t, showing.window, EventFailed)
	if !errors.Is(failure.Err, ErrInvalidPeer) {
		t.Errorf("failure = %v, want %v", failure.Err, ErrInvalidPeer)
	}
	assertNoPeer(t, showing.trust, scanner.deviceID())
}
