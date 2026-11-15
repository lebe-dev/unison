package api_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// stubClip records the content passed to it.
type stubClip struct {
	content ports.Content
	item    model.ClipItem
}

// Submit returns the prepared item.
func (s *stubClip) Submit(_ context.Context, content ports.Content) (model.ClipItem, error) {
	s.content = content
	return s.item, nil
}

// TestGetClipEmpty checks an empty history.
func TestGetClipEmpty(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1600)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})

	ctx := callTimeout(t)
	response, err := client.GetClip(ctx, connect.NewRequest(&localv1.GetClipRequest{}))
	if err != nil {
		t.Fatalf("get clip: %v", err)
	}
	if response.Msg.GetItem() != nil {
		t.Fatalf("history holds %v, want nothing", response.Msg.GetItem())
	}
}

// TestGetClipCaptured checks the item the pipeline captured.
func TestGetClipCaptured(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1700)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})

	emitClipboard(t, n, "captured")

	ctx := callTimeout(t)
	response, err := client.GetClip(ctx, connect.NewRequest(&localv1.GetClipRequest{}))
	if err != nil {
		t.Fatalf("get clip: %v", err)
	}
	item := response.Msg.GetItem()
	if item == nil {
		t.Fatal("the history is empty")
	}
	if textOf(t, item) != "captured" {
		t.Errorf("item text is %q, want %q", textOf(t, item), "captured")
	}
	if item.GetKind() != localv1.Kind_KIND_TEXT {
		t.Errorf("kind is %s, want text", item.GetKind())
	}
	if item.GetSource() != localv1.Source_SOURCE_CLIPBOARD {
		t.Errorf("source is %s, want clipboard", item.GetSource())
	}
	if item.GetCreatedAtUnixMs() == 0 || item.GetExpiresAtUnixMs() == 0 {
		t.Error("item has no time range")
	}
}

// TestSetClip checks that the text reaches the local pipeline.
func TestSetClip(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1800)
	n.start(t)

	submit := &stubClip{item: model.ClipItem{ID: model.ClipID{1}, Origin: n.DeviceID(), Kind: model.KindText}}
	client := newClient(t, n.Node, api.Options{DeviceName: "one", LocalClip: submit})

	ctx := callTimeout(t)
	response, err := client.SetClip(ctx, connect.NewRequest(&localv1.SetClipRequest{Text: "sent"}))
	if err != nil {
		t.Fatalf("set clip: %v", err)
	}
	if got := response.Msg.GetItem().GetId(); got != "01000000-0000-0000-0000-000000000000" {
		t.Errorf("item id is %q", got)
	}
	if len(submit.content.Representations) != 1 {
		t.Fatalf("submitted %d representations, want one", len(submit.content.Representations))
	}
	representation := submit.content.Representations[0]
	if representation.MIME != clip.MIMETextPlain {
		t.Errorf("mime type is %q, want %q", representation.MIME, clip.MIMETextPlain)
	}
	if string(representation.Data) != "sent" {
		t.Errorf("text is %q, want %q", representation.Data, "sent")
	}
}

// TestSetClipUnavailable checks the call without a way into the pipeline.
func TestSetClipUnavailable(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1900)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})

	ctx := callTimeout(t)
	_, err := client.SetClip(ctx, connect.NewRequest(&localv1.SetClipRequest{Text: "sent"}))
	requireCode(t, err, connect.CodeUnimplemented)
}

// TestSetClipInvalid checks the refused content.
func TestSetClipInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2000)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one", LocalClip: &stubClip{}})
	ctx := callTimeout(t)

	for name, text := range map[string]string{
		"empty":       "",
		"above limit": strings.Repeat("a", clip.MaxInlineText+1),
	} {
		_, err := client.SetClip(ctx, connect.NewRequest(&localv1.SetClipRequest{Text: text}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s text: error is %v, want invalid argument", name, err)
		}
	}
}
