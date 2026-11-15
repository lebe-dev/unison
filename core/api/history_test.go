package api_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// historyClient starts a node, serves its local API and returns the client.
func historyClient(t *testing.T) (localv1connect.UnisonServiceClient, *testNode) {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })

	n := newNode(t, network, fake.NewBus(), 41000)
	n.start(t)
	return newClient(t, n.Node, api.Options{Version: "test", DeviceName: n.name}), n
}

// emitHistory puts plain text into the clipboard of the node and waits until
// the history holds want items.
func emitHistory(t *testing.T, client localv1connect.UnisonServiceClient, n *testNode, text string, want int) []*localv1.HistoryItem {
	t.Helper()

	n.clipboard.Emit(ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
	}})

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		response, err := client.ListHistory(context.Background(), connect.NewRequest(&localv1.ListHistoryRequest{}))
		if err != nil {
			t.Fatalf("list history: %v", err)
		}
		if len(response.Msg.GetItems()) >= want {
			return response.Msg.GetItems()
		}
		n.clock.Advance(clip.DefaultDebounce)
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the history did not reach %d items", want)
	return nil
}

func TestListHistory(t *testing.T) {
	client, n := historyClient(t)
	ctx := callTimeout(t)

	emitHistory(t, client, n, "first", 1)
	items := emitHistory(t, client, n, "second", 2)

	if got := textOf(t, items[0].GetItem()); got != "second" {
		t.Fatalf("the newest item is %q", got)
	}
	if !items[0].GetComplete() {
		t.Fatal("a text item is not complete")
	}
	if got := items[0].GetItem().GetOrigin(); got != n.DeviceID().String() {
		t.Fatalf("item origin is %q, want %q", got, n.DeviceID().String())
	}

	limited, err := client.ListHistory(ctx, connect.NewRequest(&localv1.ListHistoryRequest{Limit: 1}))
	if err != nil {
		t.Fatalf("list history with a limit: %v", err)
	}
	if len(limited.Msg.GetItems()) != 1 {
		t.Fatalf("limited list has %d items, want 1", len(limited.Msg.GetItems()))
	}

	images, err := client.ListHistory(ctx, connect.NewRequest(&localv1.ListHistoryRequest{
		Kind: localv1.Kind_KIND_IMAGE,
	}))
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(images.Msg.GetItems()) != 0 {
		t.Fatalf("listed %d images, want none", len(images.Msg.GetItems()))
	}

	other, err := client.ListHistory(ctx, connect.NewRequest(&localv1.ListHistoryRequest{
		DeviceId: model.DeviceID{}.String(),
	}))
	if err != nil {
		t.Fatalf("list by device: %v", err)
	}
	if len(other.Msg.GetItems()) != 2 {
		t.Fatalf("listed %d items for a zero device, want 2", len(other.Msg.GetItems()))
	}

	_, err = client.ListHistory(ctx, connect.NewRequest(&localv1.ListHistoryRequest{DeviceId: "nope"}))
	requireCode(t, err, connect.CodeInvalidArgument)
}

func TestGetAndDeleteHistoryItem(t *testing.T) {
	client, n := historyClient(t)
	ctx := callTimeout(t)

	items := emitHistory(t, client, n, "text", 1)
	id := items[0].GetItem().GetId()

	got, err := client.GetHistoryItem(ctx, connect.NewRequest(&localv1.GetHistoryItemRequest{Id: id}))
	if err != nil {
		t.Fatalf("get history item: %v", err)
	}
	if textOf(t, got.Msg.GetItem().GetItem()) != "text" {
		t.Fatal("the item does not carry the text")
	}

	if _, err := client.DeleteHistoryItem(ctx, connect.NewRequest(&localv1.DeleteHistoryItemRequest{Id: id})); err != nil {
		t.Fatalf("delete history item: %v", err)
	}
	_, err = client.GetHistoryItem(ctx, connect.NewRequest(&localv1.GetHistoryItemRequest{Id: id}))
	requireCode(t, err, connect.CodeNotFound)

	_, err = client.GetHistoryItem(ctx, connect.NewRequest(&localv1.GetHistoryItemRequest{Id: "bad"}))
	requireCode(t, err, connect.CodeInvalidArgument)
}

func TestFetchAndCopyHistoryItem(t *testing.T) {
	client, n := historyClient(t)
	ctx := callTimeout(t)

	items := emitHistory(t, client, n, "payload", 1)
	id := items[0].GetItem().GetId()

	path := filepath.Join(t.TempDir(), "clip.txt")
	fetched, err := client.FetchHistoryItem(ctx, connect.NewRequest(&localv1.FetchHistoryItemRequest{
		Id:   id,
		Path: path,
	}))
	if err != nil {
		t.Fatalf("fetch history item: %v", err)
	}
	if files := fetched.Msg.GetFiles(); len(files) != 1 || files[0] != path {
		t.Fatalf("written files are %v, want %s", files, path)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the written payload: %v", err)
	}
	if string(written) != "payload" {
		t.Fatalf("written payload is %q", written)
	}

	if _, err := client.CopyHistoryItem(ctx, connect.NewRequest(&localv1.CopyHistoryItemRequest{Id: id})); err != nil {
		t.Fatalf("copy history item: %v", err)
	}
	content, ok := n.clipboard.LastWrite()
	if !ok {
		t.Fatal("the clipboard did not receive the item")
	}
	if got := string(content.Representations[0].Data); got != "payload" {
		t.Fatalf("clipboard holds %q", got)
	}
}
