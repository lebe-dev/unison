package api_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// requestNodes starts two Friends devices and serves the local API of both.
func requestNodes(t *testing.T, base int) (sender, receiver localv1connect.UnisonServiceClient, a, b *testNode) {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a = newNode(t, network, bus, base)
	b = newNode(t, network, bus, base+1000)
	a.start(t)
	b.start(t)
	addFriend(t, a, b)
	addFriend(t, b, a)

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if len(a.Share().Peers()) == 1 && len(b.Share().Peers()) == 1 {
			return newClient(t, a.Node, serviceOptions(a)), newClient(t, b.Node, serviceOptions(b)), a, b
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the nodes did not connect")
	return nil, nil, nil, nil
}

// addFriend stores the other device as a Friends peer.
func addFriend(t *testing.T, n *testNode, other *testNode) {
	t.Helper()

	err := n.AddPeer(t.Context(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     other.name,
		Platform: model.PlatformMacOS,
		Role:     model.RoleFriends,
	})
	if err != nil {
		t.Fatalf("add a friend: %v", err)
	}
}

// waitRequests waits until the local API lists want requests and returns the
// answer.
func waitRequests(t *testing.T, client localv1connect.UnisonServiceClient, want int) *localv1.ListRequestsResponse {
	t.Helper()

	ctx := callTimeout(t)
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		response, err := client.ListRequests(ctx, connect.NewRequest(&localv1.ListRequestsRequest{}))
		if err != nil {
			t.Fatalf("list requests: %v", err)
		}
		if len(response.Msg.GetRequests()) == want {
			return response.Msg
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the device does not list %d requests", want)
	return nil
}

func TestListAndDecideRequests(t *testing.T) {
	sender, receiver, a, b := requestNodes(t, 45000)
	ctx := callTimeout(t)

	path, data := shareSourceFile(t, "report.bin", 1<<20)
	sent, err := sender.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths:     []string{path},
		DeviceIds: []string{b.DeviceID().String()},
	}))
	if err != nil {
		t.Fatalf("send files: %v", err)
	}
	if len(sent.Msg.GetTransfers()) != 1 {
		t.Fatalf("got %d transfers, want one", len(sent.Msg.GetTransfers()))
	}

	listed := waitRequests(t, receiver, 1)
	request := listed.GetRequests()[0]
	if request.GetId() != sent.Msg.GetTransfers()[0].GetId() {
		t.Fatalf("the request id is %q", request.GetId())
	}
	if request.GetDeviceId() != a.DeviceID().String() {
		t.Fatalf("the sender is %q", request.GetDeviceId())
	}
	if request.GetDeviceName() != a.name {
		t.Fatalf("the sender name is %q", request.GetDeviceName())
	}
	if len(request.GetFiles()) != 1 || request.GetFiles()[0].GetName() != "report.bin" {
		t.Fatalf("the request holds %v", request.GetFiles())
	}
	if request.GetTotalBytes() != uint64(len(data)) {
		t.Fatalf("the request holds %d bytes, want %d", request.GetTotalBytes(), len(data))
	}
	if request.GetExpiresAtUnixMs() <= request.GetCreatedAtUnixMs() {
		t.Fatalf("the request expires at %d, created at %d", request.GetExpiresAtUnixMs(), request.GetCreatedAtUnixMs())
	}

	decided, err := receiver.DecideRequest(ctx, connect.NewRequest(&localv1.DecideRequestRequest{
		Id:     request.GetId(),
		Accept: true,
	}))
	if err != nil {
		t.Fatalf("accept the request: %v", err)
	}
	if decided.Msg.GetRequest().GetId() != request.GetId() {
		t.Fatalf("the decided request is %q", decided.Msg.GetRequest().GetId())
	}

	record := waitTransferState(t, b, request.GetId(), share.StateDone)
	if len(record.Paths) != 1 {
		t.Fatalf("the transfer saved %d files, want one", len(record.Paths))
	}
	saved, err := os.ReadFile(record.Paths[0])
	if err != nil {
		t.Fatalf("read the saved file: %v", err)
	}
	if !bytes.Equal(saved, data) {
		t.Fatalf("the saved file differs from the source (%d bytes)", len(saved))
	}
	waitRequests(t, receiver, 0)

	second, _ := shareSourceFile(t, "declined.bin", 1<<20)
	sent, err = sender.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths:     []string{second},
		DeviceIds: []string{b.DeviceID().String()},
	}))
	if err != nil {
		t.Fatalf("send files: %v", err)
	}
	listed = waitRequests(t, receiver, 1)
	request = listed.GetRequests()[0]

	if _, err := receiver.DecideRequest(ctx, connect.NewRequest(&localv1.DecideRequestRequest{
		Id:     request.GetId(),
		Accept: false,
	})); err != nil {
		t.Fatalf("decline the request: %v", err)
	}
	declined := waitTransferState(t, b, request.GetId(), share.StateDeclined)
	if len(declined.Paths) != 0 {
		t.Fatalf("a declined request saved %v", declined.Paths)
	}
	waitTransferState(t, a, sent.Msg.GetTransfers()[0].GetId(), share.StateDeclined)
	waitRequests(t, receiver, 0)

	_, err = receiver.DecideRequest(ctx, connect.NewRequest(&localv1.DecideRequestRequest{
		Id:     "00000000000000000000000000000000",
		Accept: true,
	}))
	requireCode(t, err, connect.CodeNotFound)
}
