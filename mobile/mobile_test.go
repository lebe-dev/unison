package mobile

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

func TestStartStop(t *testing.T) {
	t.Cleanup(func() {
		_ = Stop()
	})

	platform := newFakePlatform()
	cfg := marshal(t, &mobilev1.Config{DataDir: t.TempDir(), DeviceName: "phone"})
	if err := Start(cfg, platform); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := Start(cfg, platform); err == nil {
		t.Fatal("second start did not fail")
	}
	if err := Start(cfg, nil); err == nil {
		t.Fatal("start without a platform did not fail")
	}

	status := &localv1.GetStatusResponse{}
	decode(t, callRPC(t, "GetStatus", nil), status)
	if !status.GetRunning() || status.GetDeviceId() == "" || status.GetDeviceName() != "phone" {
		t.Fatalf("status %v", status)
	}

	if err := Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	response := callRPC(t, "GetStatus", nil)
	if connect.Code(response.GetCode()) != connect.CodeFailedPrecondition {
		t.Fatalf("call after stop returned code %d", response.GetCode())
	}
	if err := Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestCallDispatch(t *testing.T) {
	platform := newFakePlatform()
	core := startTestCore(t, platform)

	clipResponse := &localv1.SetClipResponse{}
	decode(t, core.call(t.Context(), &mobilev1.CallRequest{
		Method:  "unison.local.v1.UnisonService/SetClip",
		Payload: marshal(t, &localv1.SetClipRequest{Text: "hello"}),
	}), clipResponse)
	if clipResponse.GetItem().GetRepresentations()[0].GetInlineBytes() == nil {
		t.Fatal("SetClip returned no payload")
	}

	pause := &localv1.GetPauseResponse{}
	decode(t, core.call(t.Context(), &mobilev1.CallRequest{Method: "GetPause"}), pause)
	if pause.GetPause().GetPaused() {
		t.Fatal("pipeline is paused before SetPause")
	}

	decode(t, core.call(t.Context(), &mobilev1.CallRequest{
		Method:  "SetPause",
		Payload: marshal(t, &localv1.SetPauseRequest{Mode: localv1.PauseMode_PAUSE_MODE_INDEFINITE}),
	}), &localv1.SetPauseResponse{})
	decode(t, core.call(t.Context(), &mobilev1.CallRequest{Method: "GetPause"}), pause)
	if !pause.GetPause().GetPaused() || !pause.GetPause().GetIndefinite() {
		t.Fatalf("pause %v", pause.GetPause())
	}

	if response := core.call(t.Context(), &mobilev1.CallRequest{Method: "Nope"}); connect.Code(response.GetCode()) != connect.CodeUnimplemented {
		t.Fatalf("unknown method returned code %d", response.GetCode())
	}
	if response := core.call(t.Context(), &mobilev1.CallRequest{Method: "WatchEvents"}); connect.Code(response.GetCode()) != connect.CodeUnimplemented {
		t.Fatalf("WatchEvents returned code %d", response.GetCode())
	}
	if response := core.call(t.Context(), &mobilev1.CallRequest{
		Method:  "GetStatus",
		Payload: []byte{0xff},
	}); connect.Code(response.GetCode()) != connect.CodeInvalidArgument {
		t.Fatalf("invalid payload returned code %d", response.GetCode())
	}
}

func TestSubmitClipDeliversEvent(t *testing.T) {
	platform := newFakePlatform()
	cfg := marshal(t, &mobilev1.Config{DataDir: t.TempDir(), DeviceName: "phone"})
	if err := Start(cfg, platform); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop()
	})

	payload := marshal(t, &mobilev1.ClipPayload{MimeType: clip.MIMETextPlain, Data: []byte("hello")})
	if err := SubmitClip(payload); err != nil {
		t.Fatalf("submit clip: %v", err)
	}

	event := platform.waitEvent(t, mobilev1.EventType_EVENT_TYPE_CLIP_RECEIVED)
	if !event.GetEvent().GetLocal() {
		t.Fatal("clip event does not mark the item as local")
	}
	item := event.GetEvent().GetItem()
	if len(item.GetRepresentations()) != 1 || string(item.GetRepresentations()[0].GetInlineBytes()) != "hello" {
		t.Fatalf("clip event item %v", item)
	}
}

func TestSubmitClipAnnouncesToPeer(t *testing.T) {
	network := vnet.NewNetwork()
	sockets := newSocketFactory(network, 1000)
	platform := newFakePlatform()

	cfg := testConfig(t, sockets)
	cfg.dataDir = t.TempDir()
	core, err := startCore(t.Context(), cfg, platform)
	if err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := core.stop(ctx); err != nil {
			t.Errorf("stop core: %v", err)
		}
	})

	peer := startPeer(t, newSocketFactory(network, 2000))
	ctx := context.Background()
	trustPeer(t, core.node, peer.node, model.PlatformAndroid, "laptop")
	trustPeer(t, peer.node, core.node, model.PlatformMacOS, "phone")
	core.bridge.Add(string(ports.ServiceMain), peer.sockets.bound(0), nil)
	waitOnline(t, core.node, peer.node.DeviceID())

	payload := &mobilev1.ClipPayload{MimeType: clip.MIMETextPlain, Data: []byte("hello")}
	if err := core.submitClip(ctx, payload); err != nil {
		t.Fatalf("submit clip: %v", err)
	}
	waitClipboard(t, peer.clipboard, "hello")
}

// peerNode is a second device on the virtual network.
type peerNode struct {
	node      *node.Node
	clipboard *fake.Clipboard
	sockets   *socketFactory
}

// startPeer starts a node that trusts nobody yet.
func startPeer(t *testing.T, sockets *socketFactory) *peerNode {
	t.Helper()

	clipboard := fake.NewClipboard()
	created, err := node.New(node.Config{
		DataDir:      t.TempDir(),
		Name:         "laptop",
		Platform:     model.PlatformMacOS,
		Keystore:     fake.NewKeystore(),
		Clipboard:    clipboard,
		Discovery:    fake.NewBus().Discovery(),
		Logger:       discardLogger(),
		Limits:       fastLimits(),
		ListenPacket: sockets.listenPacket,
	})
	if err != nil {
		t.Fatalf("create peer: %v", err)
	}
	if err := created.Start(t.Context()); err != nil {
		t.Fatalf("start peer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := created.Stop(ctx); err != nil {
			t.Errorf("stop peer: %v", err)
		}
	})
	return &peerNode{node: created, clipboard: clipboard, sockets: sockets}
}

// trustPeer stores the other node as a Personal peer.
func trustPeer(t *testing.T, owner, other *node.Node, platform model.Platform, name string) {
	t.Helper()

	err := owner.AddPeer(context.Background(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     name,
		Platform: platform,
		Role:     model.RolePersonal,
	})
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
}

// waitOnline waits until the node has a session with the peer.
func waitOnline(t *testing.T, n *node.Node, peer model.DeviceID) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		state, err := n.Peer(context.Background(), peer)
		if err == nil && state.Online {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no session with %s within %v", peer, testTimeout)
}

// waitClipboard waits until the clipboard holds the plain text.
func waitClipboard(t *testing.T, clipboard *fake.Clipboard, want string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if content, ok := clipboard.LastWrite(); ok {
			for _, representation := range content.Representations {
				if representation.MIME == clip.MIMETextPlain && string(representation.Data) == want {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the clipboard did not receive %q within %v", want, testTimeout)
}

func TestKeystore(t *testing.T) {
	platform := newFakePlatform()
	dir := t.TempDir()

	keys, err := newKeystore(dir, platform)
	if err != nil {
		t.Fatalf("create keystore: %v", err)
	}
	if _, err := keys.Load(t.Context(), "store.dek"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("load missing secret = %v, want ErrNotFound", err)
	}

	secret := []byte("0123456789abcdef0123456789abcdef")
	if err := keys.Store(t.Context(), "personal/discovery_key", secret); err != nil {
		t.Fatalf("store secret: %v", err)
	}
	got, err := keys.Load(t.Context(), "personal/discovery_key")
	if err != nil {
		t.Fatalf("load secret: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("loaded secret differs")
	}

	reopened, err := newKeystore(dir, platform)
	if err != nil {
		t.Fatalf("reopen keystore: %v", err)
	}
	if got, err = reopened.Load(t.Context(), "personal/discovery_key"); err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("secret after reopen = %q, %v", got, err)
	}

	if err := keys.Delete(t.Context(), "personal/discovery_key"); err != nil {
		t.Fatalf("delete secret: %v", err)
	}
	if _, err := keys.Load(t.Context(), "personal/discovery_key"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("load deleted secret = %v, want ErrNotFound", err)
	}
}

func TestDiscovery(t *testing.T) {
	platform := newFakePlatform()
	clock := fake.NewClock(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
	bridge := newDiscoveryBridge(platform, clock, discardLogger())
	key := bytes.Repeat([]byte{0x42}, 32)
	bridge.key = func(context.Context) ([]byte, error) {
		return key, nil
	}
	t.Cleanup(func() {
		if err := bridge.Stop(); err != nil {
			t.Errorf("stop discovery: %v", err)
		}
	})

	if err := bridge.Advertise(t.Context(), ports.ServiceMain, 4242, nil); err != nil {
		t.Fatalf("advertise: %v", err)
	}
	main := platform.nextAnnouncement(t)
	if main.GetWithdraw() || main.GetPort() != 4242 || main.GetService() != "main" || main.GetInstance() == "" {
		t.Fatalf("announcement %v", main)
	}
	if main.GetTxt()["v"] != "1" || main.GetTxt()["tag"] == "" {
		t.Fatalf("TXT %v", main.GetTxt())
	}
	if len(main.GetInstance()) != 13 {
		t.Fatalf("instance name %q is not 13 characters", main.GetInstance())
	}

	browse, err := bridge.Browse(t.Context(), ports.ServiceMain)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	txt := marshal(t, &mobilev1.CandidateTxt{Records: map[string]string{"v": "1"}})
	bridge.Add(string(ports.ServiceMain), "127.0.0.1:4242", txt)
	select {
	case candidate := <-browse:
		if candidate.Addr != "127.0.0.1:4242" || candidate.TXT["v"] != "1" {
			t.Fatalf("candidate %v", candidate)
		}
	case <-time.After(testTimeout):
		t.Fatal("no candidate")
	}
	bridge.Add(string(ports.ServicePairing), "127.0.0.1:5555", nil)
	select {
	case candidate := <-browse:
		t.Fatalf("pairing candidate %v reached the main browse", candidate)
	case <-time.After(50 * time.Millisecond):
	}

	ad := bridge.ads[ports.ServiceMain]
	clock.Advance(10 * time.Minute)
	bridge.refreshAdvert(t.Context(), ad)
	refreshed := platform.nextAnnouncement(t)
	if refreshed.GetTxt()["tag"] == main.GetTxt()["tag"] {
		t.Fatal("tag did not change in the next epoch")
	}
	if refreshed.GetInstance() != main.GetInstance() {
		t.Fatal("instance name changed on refresh")
	}

	if err := bridge.StopService(ports.ServiceMain); err != nil {
		t.Fatalf("stop service: %v", err)
	}
	withdraw := platform.nextAnnouncement(t)
	if !withdraw.GetWithdraw() || withdraw.GetInstance() != main.GetInstance() {
		t.Fatalf("withdrawal %v", withdraw)
	}
}

// startTestCore starts a facade core on the virtual network.
func startTestCore(t *testing.T, platform *fakePlatform) *core {
	t.Helper()

	network := vnet.NewNetwork()
	sockets := newSocketFactory(network, 3000)
	core, err := startCore(t.Context(), testConfig(t, sockets), platform)
	if err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := core.stop(ctx); err != nil {
			t.Errorf("stop core: %v", err)
		}
	})
	return core
}

// callRPC runs one local API call through the facade.
func callRPC(t *testing.T, method string, payload proto.Message) *mobilev1.CallResponse {
	t.Helper()

	request := &mobilev1.CallRequest{Method: method}
	if payload != nil {
		request.Payload = marshal(t, payload)
	}
	out, err := Call(marshal(t, request))
	if err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
	response := &mobilev1.CallResponse{}
	if err := proto.Unmarshal(out, response); err != nil {
		t.Fatalf("decode call response: %v", err)
	}
	return response
}

// decode unmarshals a successful call response into msg.
func decode(t *testing.T, response *mobilev1.CallResponse, msg proto.Message) {
	t.Helper()
	if response.GetCode() != 0 {
		t.Fatalf("call failed with code %d: %s", response.GetCode(), response.GetMessage())
	}
	if err := proto.Unmarshal(response.GetPayload(), msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// marshal serializes msg.
func marshal(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
