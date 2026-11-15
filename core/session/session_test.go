package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// clipAnnounce returns a control message that announces a clip item.
func clipAnnounce(seq uint64) *wirev1.ControlMessage {
	return &wirev1.ControlMessage{
		Body: &wirev1.ControlMessage_ClipAnnounce{
			ClipAnnounce: &wirev1.ClipAnnounce{Item: &wirev1.ClipItem{Seq: seq}},
		},
	}
}

// TestHelloExchange checks that both sides announce themselves and that the
// peer record is updated.
func TestHelloExchange(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	h.clientCfg.Capabilities = []string{"clip", "blob"}
	h.clientCfg.RequireConfirmation = true
	h.serverCfg.Name = "workstation"

	client, server := h.open(nil)

	hello := client.PeerHello()
	if hello.Version != ProtocolVersion {
		t.Fatalf("peer version = %d, want %d", hello.Version, ProtocolVersion)
	}
	if hello.Name != "workstation" {
		t.Fatalf("peer name = %q, want workstation", hello.Name)
	}
	if hello.Platform != model.PlatformWindows {
		t.Fatalf("peer platform = %s, want windows", hello.Platform)
	}
	if !server.PeerHello().RequireConfirmation {
		t.Fatal("peer self policy lost the confirmation flag")
	}
	if got := server.PeerHello().Capabilities; len(got) != 2 || got[0] != "clip" || got[1] != "blob" {
		t.Fatalf("capabilities = %v, want [clip blob]", got)
	}
	if got := server.PeerHello(); got.Name != "client" || got.Platform != model.PlatformMacOS {
		t.Fatalf("peer hello = %+v, want the client on macOS", got)
	}

	// The Hello name and platform reach the trust store.
	if got := h.serverTrust.peer(t, h.clientID.DeviceID()); got.Name != "client" || got.Platform != model.PlatformMacOS {
		t.Fatalf("stored peer = %q on %s, want the announced name and platform", got.Name, got.Platform)
	}
	if got := h.clientTrust.peer(t, h.serverID.DeviceID()); got.Name != "workstation" || got.Platform != model.PlatformWindows {
		t.Fatalf("stored peer = %q on %s, want the announced name and platform", got.Name, got.Platform)
	}

	for _, s := range []*Session{client, server} {
		event := waitEvent(t, s, EventOpened)
		if event.Peer != s.DeviceID() {
			t.Fatalf("opened event peer = %s, want %s", event.Peer, s.DeviceID())
		}
	}
}

// TestIncompatibleVersion checks that a peer with another protocol version is
// asked to update and the session is closed.
func TestIncompatibleVersion(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	hello := &wirev1.Hello{ProtocolVersion: ProtocolVersion + 1, Name: "old"}

	server, closed := h.rawOpen(hello)

	if err := <-closed; !errors.Is(err, ErrIncompatibleVersion) {
		t.Fatalf("open error = %v, want ErrIncompatibleVersion", err)
	}
	event := waitEvent(t, server, EventPeerNeedsUpdate)
	if event.Version != ProtocolVersion+1 {
		t.Fatalf("event version = %d, want %d", event.Version, ProtocolVersion+1)
	}

	code := waitCloseCode(t, h.clientConn.ControlStream())
	if code != CodeIncompatibleVersion {
		t.Fatalf("close code = %#x, want %#x", code, CodeIncompatibleVersion)
	}
	if err := server.Err(); !errors.Is(err, ErrIncompatibleVersion) {
		t.Fatalf("session error = %v, want ErrIncompatibleVersion", err)
	}
}

// TestHelloRejectsInvalidName checks that a Hello with an unusable name ends the
// session.
func TestHelloRejectsInvalidName(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	hello := &wirev1.Hello{ProtocolVersion: ProtocolVersion, Name: string(make([]byte, MaxNameLength+1))}

	server, closed := h.rawOpen(hello)

	if err := <-closed; !errors.Is(err, ErrInvalidHello) {
		t.Fatalf("open error = %v, want ErrInvalidHello", err)
	}
	if code := waitCloseCode(t, h.clientConn.ControlStream()); code != CodeProtocolViolation {
		t.Fatalf("close code = %#x, want %#x", code, CodeProtocolViolation)
	}
	waitDone(t, server)
}

// TestFriendsClipAnnounceDenied checks that a Friends peer cannot announce a
// clip and that repeated attempts end the session.
func TestFriendsClipAnnounceDenied(t *testing.T) {
	h := newHarness(t, model.RoleFriends, model.RoleFriends)
	var handled atomic.Int32

	client, server := h.open(func(_, server *Session) {
		if err := server.Handle(KindClipAnnounce, func(context.Context, *wirev1.ControlMessage) error {
			handled.Add(1)
			return nil
		}); err != nil {
			t.Fatalf("handle clip announce: %v", err)
		}
	})

	for i := 1; i <= MaxViolations; i++ {
		if err := client.Send(context.Background(), clipAnnounce(uint64(i))); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	waitDone(t, server)
	if err := server.Err(); !errors.Is(err, ErrTooManyViolations) {
		t.Fatalf("session error = %v, want ErrTooManyViolations", err)
	}
	if got := server.Violations(); got != MaxViolations {
		t.Fatalf("violations = %d, want %d", got, MaxViolations)
	}
	if got := handled.Load(); got != 0 {
		t.Fatalf("handler ran %d times, want 0", got)
	}
	event := waitEvent(t, server, EventClosed)
	if !errors.Is(event.Err, ErrTooManyViolations) {
		t.Fatalf("closed event = %v, want ErrTooManyViolations", event.Err)
	}
	waitDone(t, client)
	if code := waitCloseCode(t, h.clientConn.ControlStream()); code != CodeProtocolViolation {
		t.Fatalf("close code = %#x, want %#x", code, CodeProtocolViolation)
	}
}

// TestPersonalClipAnnounceDelivered checks that an allowed message reaches its
// handler.
func TestPersonalClipAnnounceDelivered(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	delivered := make(chan uint64, 1)

	client, _ := h.open(func(_, server *Session) {
		if err := server.Handle(KindClipAnnounce, func(_ context.Context, msg *wirev1.ControlMessage) error {
			delivered <- msg.GetClipAnnounce().GetItem().GetSeq()
			return nil
		}); err != nil {
			t.Fatalf("handle clip announce: %v", err)
		}
	})

	if err := client.Send(context.Background(), clipAnnounce(7)); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case seq := <-delivered:
		if seq != 7 {
			t.Fatalf("sequence = %d, want 7", seq)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the announcement did not reach the handler")
	}
}

// TestClipAnnounceRateLimited checks that announcements over the burst are
// dropped and counted as violations.
func TestClipAnnounceRateLimited(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	h.serverCfg.Clock = fake.NewClock(time.Now())

	var handled atomic.Int32
	done := make(chan struct{})
	client, server := h.open(func(_, server *Session) {
		if err := server.Handle(KindClipAnnounce, func(context.Context, *wirev1.ControlMessage) error {
			if handled.Add(1) == ClipAnnounceBurst {
				close(done)
			}
			return nil
		}); err != nil {
			t.Fatalf("handle clip announce: %v", err)
		}
	})

	for i := 1; i <= ClipAnnounceBurst; i++ {
		if err := client.Send(context.Background(), clipAnnounce(uint64(i))); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("handled %d announcements, want %d", handled.Load(), ClipAnnounceBurst)
	}

	if err := client.Send(context.Background(), clipAnnounce(ClipAnnounceBurst+1)); err != nil {
		t.Fatalf("send: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for server.Violations() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the announcement over the burst was not counted as a violation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := handled.Load(); got != ClipAnnounceBurst {
		t.Fatalf("handled %d announcements, want %d", got, ClipAnnounceBurst)
	}
	if server.isClosed() {
		t.Fatal("the session ended after one violation")
	}
}

// TestRoleChangeDenied checks that the matrix is read for every message: a peer
// whose role changed in the trust store loses the capability at once.
func TestRoleChangeDenied(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	var handled atomic.Int32

	client, server := h.open(func(_, server *Session) {
		if err := server.Handle(KindClipAnnounce, func(context.Context, *wirev1.ControlMessage) error {
			handled.Add(1)
			return nil
		}); err != nil {
			t.Fatalf("handle clip announce: %v", err)
		}
	})

	if err := client.Send(context.Background(), clipAnnounce(1)); err != nil {
		t.Fatalf("send: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for handled.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first announcement did not arrive")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := h.serverTrust.update(h.clientID.DeviceID(), func(peer *trust.Peer) { peer.Role = model.RoleFriends }); err != nil {
		t.Fatalf("change role: %v", err)
	}
	if err := client.Send(context.Background(), clipAnnounce(2)); err != nil {
		t.Fatalf("send: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for server.Violations() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the announcement was not denied after the role change")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := handled.Load(); got != 1 {
		t.Fatalf("handled %d announcements, want 1", got)
	}
}

// TestOversizedControlFrame checks that a frame over the control message limit
// ends the session before the payload is read.
func TestOversizedControlFrame(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	server, closed := h.rawOpen(&wirev1.Hello{ProtocolVersion: ProtocolVersion, Name: "client"})
	if err := <-closed; err != nil {
		t.Fatalf("open: %v", err)
	}

	// Announce a payload over the limit and send nothing of it.
	stream := h.clientConn.ControlStream()
	if _, err := stream.Write([]byte{0xff, 0xff, 0x7f}); err != nil {
		t.Fatalf("write length prefix: %v", err)
	}

	waitDone(t, server)
	if err := server.Err(); !errors.Is(err, ErrFrame) {
		t.Fatalf("session error = %v, want ErrFrame", err)
	}
	if code := waitCloseCode(t, h.clientConn.ControlStream()); code != CodeProtocolViolation {
		t.Fatalf("close code = %#x, want %#x", code, CodeProtocolViolation)
	}
}

// TestCorruptControlFrame checks that a payload which is not a valid control
// message ends the session.
func TestCorruptControlFrame(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	server, closed := h.rawOpen(&wirev1.Hello{ProtocolVersion: ProtocolVersion, Name: "client"})
	if err := <-closed; err != nil {
		t.Fatalf("open: %v", err)
	}

	// Field 1 of the envelope with a truncated length and a broken varint.
	if _, err := h.clientConn.ControlStream().Write([]byte{0x04, 0x0a, 0xff, 0xff, 0xff, 0xff}); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	waitDone(t, server)
	if err := server.Err(); !errors.Is(err, ErrMessage) {
		t.Fatalf("session error = %v, want ErrMessage", err)
	}
	if code := waitCloseCode(t, h.clientConn.ControlStream()); code != CodeProtocolViolation {
		t.Fatalf("close code = %#x, want %#x", code, CodeProtocolViolation)
	}
}

// TestUnknownControlMessage checks that an envelope without a payload is
// counted as a violation and dropped.
func TestUnknownControlMessage(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	server, closed := h.rawOpen(&wirev1.Hello{ProtocolVersion: ProtocolVersion, Name: "client"})
	if err := <-closed; err != nil {
		t.Fatalf("open: %v", err)
	}

	// A valid protobuf message with an unknown field and no body.
	if _, err := h.clientConn.ControlStream().Write([]byte{0x02, 0x68, 0x01}); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for server.Violations() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the message without a body was not counted as a violation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if server.isClosed() {
		t.Fatal("the session ended after one violation")
	}
}

// TestSendRefused checks the outgoing validation.
func TestSendRefused(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	client, _ := h.open(nil)
	ctx := context.Background()

	hello := &wirev1.ControlMessage{Body: &wirev1.ControlMessage_Hello{Hello: &wirev1.Hello{}}}
	if err := client.Send(ctx, hello); !errors.Is(err, ErrHandler) {
		t.Fatalf("send hello error = %v, want ErrHandler", err)
	}
	if err := client.Send(ctx, &wirev1.ControlMessage{}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("send empty error = %v, want ErrUnknownKind", err)
	}

	large := clipAnnounce(1)
	large.GetClipAnnounce().Item.Representations = []*wirev1.Representation{
		{InlineBytes: make([]byte, MaxControlMessage)},
	}
	if err := client.Send(ctx, large); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("send large error = %v, want ErrMessageTooLarge", err)
	}
}

// TestHandlerRegistration checks the registration rules.
func TestHandlerRegistration(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	client, err := New(h.clientConn, h.clientCfg)
	if err != nil {
		t.Fatalf("session: %v", err)
	}

	handler := func(context.Context, *wirev1.ControlMessage) error { return nil }
	for _, kind := range []Kind{KindUnknown, KindHello, KindAuthProof} {
		if err := client.Handle(kind, handler); !errors.Is(err, ErrHandler) {
			t.Fatalf("handle %s error = %v, want ErrHandler", kind, err)
		}
	}
	if err := client.Handle(KindClipAnnounce, nil); !errors.Is(err, ErrHandler) {
		t.Fatalf("nil handler error = %v, want ErrHandler", err)
	}
	if err := client.Handle(KindClipAnnounce, handler); err != nil {
		t.Fatalf("handle clip announce: %v", err)
	}
	if err := client.Handle(KindClipAnnounce, handler); !errors.Is(err, ErrHandler) {
		t.Fatalf("second handler error = %v, want ErrHandler", err)
	}

	streamHandler := func(context.Context, *Request) error { return nil }
	if err := client.HandleRequest("", trust.ActionSyncHistory, streamHandler); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("empty payload type error = %v, want ErrInvalidHeader", err)
	}
	if err := client.HandleRequest("share", trust.ActionNone, streamHandler); err != nil {
		t.Fatalf("handle request without an action: %v", err)
	}
	if err := client.HandleRequest("blob", trust.ActionFetchBlob, streamHandler); err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if err := client.HandleRequest("blob", trust.ActionFetchBlob, streamHandler); !errors.Is(err, ErrHandler) {
		t.Fatalf("second request handler error = %v, want ErrHandler", err)
	}
}

// TestClosedSession checks that a closed session refuses new work.
func TestClosedSession(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	client, _ := h.open(nil)
	client.Close()

	if err := client.Send(context.Background(), clipAnnounce(1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("send error = %v, want ErrClosed", err)
	}
	if _, err := client.OpenRequest(context.Background(), &wirev1.StreamHeader{PayloadType: "blob"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("open request error = %v, want ErrClosed", err)
	}
	if err := client.Handle(KindClipAnnounce, func(context.Context, *wirev1.ControlMessage) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("handle error = %v, want ErrClosed", err)
	}
	if err := client.Open(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("second open error = %v, want ErrClosed", err)
	}
	if err := client.Err(); err != nil {
		t.Fatalf("session error = %v, want nil after a local close", err)
	}
}

// TestNewRefused checks the constructor validation.
func TestNewRefused(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	long := make([]byte, MaxNameLength+1)
	for i := range long {
		long[i] = 'a'
	}

	if _, err := New(nil, Config{Trust: h.clientTrust}); err == nil {
		t.Fatal("session without a connection was created")
	}
	if _, err := New(h.clientConn, Config{}); err == nil {
		t.Fatal("session without a trust store was created")
	}
	if _, err := New(h.clientConn, Config{Trust: h.clientTrust, Name: string(long)}); !errors.Is(err, ErrInvalidHello) {
		t.Fatalf("long name error = %v, want ErrInvalidHello", err)
	}
	capabilities := make([]string, MaxCapabilities+1)
	for i := range capabilities {
		capabilities[i] = "x"
	}
	if _, err := New(h.clientConn, Config{Trust: h.clientTrust, Capabilities: capabilities}); !errors.Is(err, ErrInvalidHello) {
		t.Fatalf("too many capabilities error = %v, want ErrInvalidHello", err)
	}
}

// TestCloseCodeValues keeps the session codes apart from the transport ones.
func TestCloseCodeValues(t *testing.T) {
	if CodeIncompatibleVersion == 0 || CodeProtocolViolation == 0 {
		t.Fatal("session close codes must not be zero")
	}
	if CodeIncompatibleVersion == CodeProtocolViolation {
		t.Fatal("session close codes must differ")
	}
}
