package mobile

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/clip"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// TestHistoryThumbnail checks that the dashboard receives the pixels of an
// image entry through the facade, which asks the core to decode them.
func TestHistoryThumbnail(t *testing.T) {
	platform := newFakePlatform()
	core := startTestCore(t, platform)

	source := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for x := range 32 {
		for y := range 16 {
			source.SetNRGBA(x, y, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	payload := &mobilev1.ClipPayload{
		MimeType: clip.MIMEImagePNG,
		Parts:    []*mobilev1.ClipPart{{MimeType: clip.MIMEImagePNG, Data: buffer.Bytes()}},
	}
	if err := core.submitClip(t.Context(), payload); err != nil {
		t.Fatalf("submit clip: %v", err)
	}

	id := waitImage(t, core)
	thumbnail := &localv1.GetHistoryThumbnailResponse{}
	decode(t, core.call(t.Context(), &mobilev1.CallRequest{
		Method:  "GetHistoryThumbnail",
		Payload: marshal(t, &localv1.GetHistoryThumbnailRequest{Id: id, MaxSide: 16}),
	}), thumbnail)
	if thumbnail.GetWidth() != 16 || thumbnail.GetHeight() != 8 {
		t.Fatalf("thumbnail is %dx%d, want 16x8", thumbnail.GetWidth(), thumbnail.GetHeight())
	}
	rgba := thumbnail.GetRgba()
	if len(rgba) != 16*8*4 {
		t.Fatalf("thumbnail holds %d bytes", len(rgba))
	}
	if first := rgba[:4]; !bytes.Equal(first, []byte{0x10, 0x20, 0x30, 0xff}) {
		t.Fatalf("first pixel is %v", first)
	}
}

// waitImage waits until an image entry reaches the history and returns its ID.
func waitImage(t *testing.T, core *core) string {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		list := &localv1.ListHistoryResponse{}
		decode(t, core.call(t.Context(), &mobilev1.CallRequest{
			Method:  "ListHistory",
			Payload: marshal(t, &localv1.ListHistoryRequest{Kind: localv1.Kind_KIND_IMAGE}),
		}), list)
		if items := list.GetItems(); len(items) > 0 {
			return items[0].GetItem().GetId()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the image did not reach the history")
	return ""
}
