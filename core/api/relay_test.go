package api_test

import (
	"net"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestRelaySettings checks that the relay access is stored and reported without
// the token.
func TestRelaySettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 300)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	initial, err := client.GetRelay(ctx, connect.NewRequest(&localv1.GetRelayRequest{}))
	if err != nil {
		t.Fatalf("get relay: %v", err)
	}
	if initial.Msg.GetRelay().GetUrl() != "" || initial.Msg.GetRelay().GetTokenSet() {
		t.Fatalf("the relay is configured before any change: %+v", initial.Msg.GetRelay())
	}
	if !initial.Msg.GetRelay().GetFilesOnRequest() {
		t.Error("files over the relay are allowed without a request by default")
	}

	token := "group-token"
	saved, err := client.SetRelay(ctx, connect.NewRequest(&localv1.SetRelayRequest{
		Url:   "https://relay.example:8443",
		Token: &token,
	}))
	if err != nil {
		t.Fatalf("set relay: %v", err)
	}
	relay := saved.Msg.GetRelay()
	if relay.GetUrl() != "https://relay.example:8443" {
		t.Errorf("relay address is %q", relay.GetUrl())
	}
	if !relay.GetTokenSet() {
		t.Error("the response does not report the stored token")
	}
	if relay.GetConnected() {
		t.Error("the response reports a connection without a relay")
	}
	if relay.GetChangedAtUnixMs() == 0 {
		t.Error("the response does not report the change time")
	}

	stored, _, err := n.RelaySettings(ctx)
	if err != nil {
		t.Fatalf("read the relay settings: %v", err)
	}
	if stored != (trust.RelaySettings{URL: "https://relay.example:8443", Token: token}) {
		t.Errorf("stored relay is %+v", stored)
	}

	// A request without a token keeps the stored one.
	other, err := client.SetRelay(ctx, connect.NewRequest(&localv1.SetRelayRequest{
		Url: "relay.example:443",
	}))
	if err != nil {
		t.Fatalf("set the relay address only: %v", err)
	}
	if other.Msg.GetRelay().GetUrl() != "relay.example:443" || !other.Msg.GetRelay().GetTokenSet() {
		t.Errorf("the second change lost the token: %+v", other.Msg.GetRelay())
	}

	off, err := client.RelayOff(ctx, connect.NewRequest(&localv1.RelayOffRequest{}))
	if err != nil {
		t.Fatalf("turn the relay off: %v", err)
	}
	if off.Msg.GetRelay().GetUrl() != "" || off.Msg.GetRelay().GetTokenSet() {
		t.Errorf("the relay is still configured: %+v", off.Msg.GetRelay())
	}
}

// TestRelayFilesRule checks that the rule for files over the relay is stored and
// applied without a restart.
func TestRelayFilesRule(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 304)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	token := "group-token"
	allowed := false
	response, err := client.SetRelay(ctx, connect.NewRequest(&localv1.SetRelayRequest{
		Url:            "relay.example:443",
		Token:          &token,
		FilesOnRequest: &allowed,
	}))
	if err != nil {
		t.Fatalf("set relay: %v", err)
	}
	if response.Msg.GetRelay().GetFilesOnRequest() {
		t.Error("the rule did not change")
	}
	if n.RelayFilesOnRequest() {
		t.Error("the node still keeps files over the relay")
	}
	if n.Policy().Config().RelayFilesOnRequest {
		t.Error("the policy did not change")
	}
}

// pointer returns a pointer to value.
func pointer[T any](value T) *T {
	return &value
}

// TestRelayRefusesBadSettings checks the validation of the relay address and
// token.
func TestRelayRefusesBadSettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 308)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	token := "group-token"
	empty := ""
	cases := map[string]*localv1.SetRelayRequest{
		"no address":        {Token: &token},
		"plain scheme":      {Url: "http://relay.example", Token: &token},
		"with a path":       {Url: "https://relay.example/relay", Token: &token},
		"with a user":       {Url: "https://user@relay.example", Token: &token},
		"bad port":          {Url: "relay.example:http", Token: &token},
		"port out of range": {Url: "relay.example:70000", Token: &token},
		"no token":          {Url: "relay.example:443"},
		"empty token":       {Url: "relay.example:443", Token: &empty},
		"long address": {
			Url:   "relay.example:443/" + strings.Repeat("a", trust.MaxRelayURLLength),
			Token: &token,
		},
		"long token": {
			Url:   "relay.example:443",
			Token: pointer(strings.Repeat("t", trust.MaxRelayTokenLength+1)),
		},
	}
	for name, request := range cases {
		if _, err := client.SetRelay(ctx, connect.NewRequest(request)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: error %v, want invalid argument", name, err)
		}
	}
	if stored, _, err := n.RelaySettings(ctx); err != nil || !stored.Empty() {
		t.Errorf("a refused request stored the relay: %+v, error %v", stored, err)
	}
}

// TestRelayCheckRefusesBadSettings checks that a check without a usable address
// or token is refused before anything is dialed.
func TestRelayCheckRefusesBadSettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 309)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	token := "group-token"
	cases := map[string]*localv1.TestRelayRequest{
		"nothing stored": {},
		"no token":       {Url: "relay.example:443"},
		"plain scheme":   {Url: "http://relay.example", Token: &token},
		"with a path":    {Url: "https://relay.example/relay", Token: &token},
	}
	for name, request := range cases {
		if _, err := client.TestRelay(ctx, connect.NewRequest(request)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: error %v, want invalid argument", name, err)
		}
	}
}

// TestRelayCheckReportsEachTransport checks that a check of the stored relay
// reports the failure of each transport and stores nothing.
func TestRelayCheckReportsEachTransport(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 310)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: n.name})
	ctx := callTimeout(t)

	// Nothing listens on the port, so both transports fail.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	token := "group-token"
	if _, err := client.SetRelay(ctx, connect.NewRequest(&localv1.SetRelayRequest{Url: address, Token: &token})); err != nil {
		t.Fatalf("set relay: %v", err)
	}

	response, err := client.TestRelay(ctx, connect.NewRequest(&localv1.TestRelayRequest{}))
	if err != nil {
		t.Fatalf("check relay: %v", err)
	}
	quic := response.Msg.GetQuic()
	if quic.GetTransport() != localv1.RelayTransport_RELAY_TRANSPORT_QUIC || quic.GetOk() || quic.GetDetail() == "" {
		t.Errorf("QUIC check is %+v", quic)
	}
	websocket := response.Msg.GetWebsocket()
	if websocket.GetTransport() != localv1.RelayTransport_RELAY_TRANSPORT_WSS || websocket.GetOk() {
		t.Errorf("WebSocket check is %+v", websocket)
	}
	if websocket.GetFailure() != localv1.RelayCheckFailure_RELAY_CHECK_FAILURE_REFUSED || websocket.GetDetail() == "" {
		t.Errorf("WebSocket check failure is %s, detail %q", websocket.GetFailure(), websocket.GetDetail())
	}

	// Another address with the stored token is checked without being stored.
	other := "127.0.0.1:1"
	if _, err := client.TestRelay(ctx, connect.NewRequest(&localv1.TestRelayRequest{Url: other})); err != nil {
		t.Fatalf("check another relay with the stored token: %v", err)
	}
	if stored, _, err := n.RelaySettings(ctx); err != nil || stored.URL != address {
		t.Errorf("the check changed the stored relay: %+v, error %v", stored, err)
	}
}
