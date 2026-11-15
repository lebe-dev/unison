package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// transferNodes starts two paired nodes and serves the local API of the first
// one.
func transferNodes(t *testing.T, base int) (localv1connect.UnisonServiceClient, *testNode, *testNode) {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newNode(t, network, bus, base)
	b := newNode(t, network, bus, base+1000)
	a.start(t)
	b.start(t)
	a.pair(t, b)
	b.pair(t, a)

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if len(a.Share().Peers()) == 1 && len(b.Share().Peers()) == 1 {
			return newClient(t, a.Node, serviceOptions(a)), a, b
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the nodes did not connect")
	return nil, nil, nil
}

// waitTransferState waits for the transfer to reach the state.
func waitTransferState(t *testing.T, n *testNode, id string, state share.State) share.Transfer {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		record, err := n.Share().Transfer(id)
		if err == nil && record.State == state {
			return record
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, err := n.Share().Transfer(id)
	t.Fatalf("transfer is %s (%v), want %s", record.State, err, state)
	return share.Transfer{}
}

// shareSourceFile writes a file and returns its path and content.
func shareSourceFile(t *testing.T, name string, size int) (string, []byte) {
	t.Helper()

	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	return path, data
}

func TestSendFiles(t *testing.T) {
	client, a, b := transferNodes(t, 43000)
	ctx := callTimeout(t)

	path, data := shareSourceFile(t, "notes.txt", 2<<20)
	response, err := client.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths:     []string{path},
		DeviceIds: []string{b.DeviceID().String()},
	}))
	if err != nil {
		t.Fatalf("send files: %v", err)
	}
	if len(response.Msg.GetTransfers()) != 1 {
		t.Fatalf("got %d transfers, want one", len(response.Msg.GetTransfers()))
	}
	transfer := response.Msg.GetTransfers()[0]
	if transfer.GetDirection() != localv1.TransferDirection_TRANSFER_DIRECTION_OUTGOING {
		t.Fatalf("the direction is %s", transfer.GetDirection())
	}
	if transfer.GetDeviceId() != b.DeviceID().String() {
		t.Fatalf("the receiver is %q", transfer.GetDeviceId())
	}
	if len(transfer.GetFiles()) != 1 || transfer.GetFiles()[0].GetName() != "notes.txt" {
		t.Fatalf("the transfer holds %v", transfer.GetFiles())
	}
	if transfer.GetTotalBytes() != uint64(len(data)) {
		t.Fatalf("the transfer holds %d bytes, want %d", transfer.GetTotalBytes(), len(data))
	}

	incoming := waitIncomingTransfer(t, b, a.DeviceID())
	if incoming.GetState() != localv1.TransferState_TRANSFER_STATE_DONE {
		t.Fatalf("the incoming transfer is %s", incoming.GetState())
	}
	saved, err := os.ReadFile(incoming.GetPaths()[0])
	if err != nil {
		t.Fatalf("read the saved file: %v", err)
	}
	if !bytes.Equal(saved, data) {
		t.Fatalf("the saved file differs from the source (%d bytes)", len(saved))
	}

	listed, err := client.ListTransfers(ctx, connect.NewRequest(&localv1.ListTransfersRequest{}))
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if len(listed.Msg.GetTransfers()) != 1 {
		t.Fatalf("listed %d transfers, want one", len(listed.Msg.GetTransfers()))
	}
	if listed.Msg.GetTransfers()[0].GetState() != localv1.TransferState_TRANSFER_STATE_DONE {
		t.Fatalf("the listed transfer is %s", listed.Msg.GetTransfers()[0].GetState())
	}
}

// waitIncomingTransfer waits for the incoming transfer of the peer.
func waitIncomingTransfer(t *testing.T, n *testNode, peer model.DeviceID) *localv1.Transfer {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		for _, record := range n.Share().Transfers() {
			if record.Peer != peer || record.Direction != share.DirectionIncoming {
				continue
			}
			if record.State != share.StateDone {
				continue
			}
			return transferOf(t, n, record.ID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no finished incoming transfer from %s", peer)
	return nil
}

// transferOf converts one transfer of the node.
func transferOf(t *testing.T, n *testNode, id string) *localv1.Transfer {
	t.Helper()

	client := newClient(t, n.Node, serviceOptions(n))
	response, err := client.ListTransfers(context.Background(), connect.NewRequest(&localv1.ListTransfersRequest{}))
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	for _, transfer := range response.Msg.GetTransfers() {
		if transfer.GetId() == id {
			return transfer
		}
	}
	t.Fatalf("transfer %s is not listed", id)
	return nil
}

func TestTransferErrors(t *testing.T) {
	client, _, b := transferNodes(t, 45000)
	ctx := callTimeout(t)

	path, _ := shareSourceFile(t, "file.bin", 1024)
	_, err := client.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths:     []string{path},
		DeviceIds: []string{"nope"},
	}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths: []string{path},
	}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.CancelTransfer(ctx, connect.NewRequest(&localv1.CancelTransferRequest{Id: "unknown"}))
	requireCode(t, err, connect.CodeNotFound)

	_, err = client.AcceptTransfer(ctx, connect.NewRequest(&localv1.AcceptTransferRequest{Id: "unknown"}))
	requireCode(t, err, connect.CodeNotFound)

	response, err := client.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths:     []string{path},
		DeviceIds: []string{b.DeviceID().String()},
	}))
	if err != nil {
		t.Fatalf("send files: %v", err)
	}
	id := response.Msg.GetTransfers()[0].GetId()
	waitTransferState(t, b, id, share.StateDone)

	_, err = client.CancelTransfer(ctx, connect.NewRequest(&localv1.CancelTransferRequest{Id: id}))
	requireCode(t, err, connect.CodeFailedPrecondition)
}

func TestTransferEvent(t *testing.T) {
	client, _, b := transferNodes(t, 47000)
	ctx := callTimeout(t)
	events := watch(t, client)

	path, _ := shareSourceFile(t, "report.txt", 1024)
	if _, err := client.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths:     []string{path},
		DeviceIds: []string{b.DeviceID().String()},
	})); err != nil {
		t.Fatalf("send files: %v", err)
	}

	event := waitEvent(t, events, localv1.EventKind_EVENT_KIND_TRANSFER)
	if event.GetTransfer() == nil {
		t.Fatal("the transfer event carries no transfer")
	}
	if event.GetDeviceId() != b.DeviceID().String() {
		t.Fatalf("the event is about %q", event.GetDeviceId())
	}
}

// serviceOptions returns the local API options of a node.
func serviceOptions(n *testNode) api.Options {
	return api.Options{Version: "test", DeviceName: n.name}
}
