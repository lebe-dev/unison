package api_test

import (
	"testing"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// TestSetFriendsDiscovery checks the state of the temporary search in the status
// and in the answer of the call.
func TestSetFriendsDiscovery(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 300)
	n.start(t)
	client := newClient(t, n.Node, api.Options{Version: "1.2.3", DeviceName: "workstation"})
	ctx := callTimeout(t)

	status := getStatus(t, client)
	if status.GetFriendsDiscovery().GetOn() {
		t.Error("a fresh daemon reports the search for friends")
	}

	response, err := client.SetFriendsDiscovery(ctx, connect.NewRequest(&localv1.SetFriendsDiscoveryRequest{On: true}))
	if err != nil {
		t.Fatalf("set friends discovery: %v", err)
	}
	search := response.Msg.GetFriendsDiscovery()
	if !search.GetOn() {
		t.Fatal("the search is off after it was turned on")
	}
	first := search.GetUntilUnixMs()
	if first <= n.clock.Now().UnixMilli() {
		t.Fatalf("the search ends at %d, before now", first)
	}

	// A second call extends the running search.
	response, err = client.SetFriendsDiscovery(ctx, connect.NewRequest(&localv1.SetFriendsDiscoveryRequest{On: true}))
	if err != nil {
		t.Fatalf("extend friends discovery: %v", err)
	}
	if extended := response.Msg.GetFriendsDiscovery().GetUntilUnixMs(); extended <= first {
		t.Fatalf("the search ends at %d after the extension, want above %d", extended, first)
	}

	status = getStatus(t, client)
	if !status.GetFriendsDiscovery().GetOn() {
		t.Fatal("the status reports the search as off while it runs")
	}
	if status.GetFriendsDiscovery().GetUntilUnixMs() != response.Msg.GetFriendsDiscovery().GetUntilUnixMs() {
		t.Fatalf("the status reports the end %d, want %d",
			status.GetFriendsDiscovery().GetUntilUnixMs(), response.Msg.GetFriendsDiscovery().GetUntilUnixMs())
	}

	response, err = client.SetFriendsDiscovery(ctx, connect.NewRequest(&localv1.SetFriendsDiscoveryRequest{On: false}))
	if err != nil {
		t.Fatalf("turn friends discovery off: %v", err)
	}
	if response.Msg.GetFriendsDiscovery().GetOn() || response.Msg.GetFriendsDiscovery().GetUntilUnixMs() != 0 {
		t.Fatalf("the search is still on after it was turned off: %v", response.Msg.GetFriendsDiscovery())
	}
	if getStatus(t, client).GetFriendsDiscovery().GetOn() {
		t.Fatal("the status reports the search after it was turned off")
	}
}

// TestFriendsDiscoveryWithoutKeys checks that the search runs without a Friends
// device and announces nothing.
func TestFriendsDiscoveryWithoutKeys(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 400)
	n.start(t)
	client := newClient(t, n.Node, api.Options{})

	ctx := callTimeout(t)
	response, err := client.SetFriendsDiscovery(ctx, connect.NewRequest(&localv1.SetFriendsDiscoveryRequest{On: true}))
	if err != nil {
		t.Fatalf("set friends discovery: %v", err)
	}
	if !response.Msg.GetFriendsDiscovery().GetOn() {
		t.Fatal("the search is off without a Friends device")
	}
	if on, _, err := n.FriendsDiscovery(ctx); err != nil || !on {
		t.Fatalf("the node reports the search as off (%v)", err)
	}
}

// getStatus reads the status of the daemon.
func getStatus(t *testing.T, client localv1connect.UnisonServiceClient) *localv1.GetStatusResponse {
	t.Helper()

	response, err := client.GetStatus(callTimeout(t), connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	return response.Msg
}
