package node

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/image/tiff"
	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
)

// clipNodes starts two paired nodes of the virtual network.
func clipNodes(t *testing.T, base int) (*testNode, *testNode) {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNode(t, network, bus, base)
	b := newTestNode(t, network, bus, base+1000)
	a.start(t)
	b.start(t)

	a.pair(t, b)
	b.pair(t, a)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())
	small, large := smaller(a, b)
	waitSingleConnection(t, small, large)
	return a, b
}

// testImage draws a small test image.
func testImage(width, height int) image.Image {
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			source.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 6), B: 0x30, A: 0xff})
		}
	}
	return source
}

// pngBytes encodes a test image as PNG.
func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, testImage(width, height)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

// tiffBytes encodes a test image as TIFF.
func tiffBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := tiff.Encode(&buffer, testImage(width, height), nil); err != nil {
		t.Fatalf("encode tiff: %v", err)
	}
	return buffer.Bytes()
}

// noisyImage draws an image that does not compress.
func noisyImage(width, height int) image.Image {
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	seed := rand.New(rand.NewSource(1))
	for y := range height {
		for x := range width {
			source.SetNRGBA(x, y, color.NRGBA{
				R: uint8(seed.Intn(256)),
				G: uint8(seed.Intn(256)),
				B: uint8(seed.Intn(256)),
				A: 0xff,
			})
		}
	}
	return source
}

// imageSnapshot returns clipboard content with one image.
func imageSnapshot(mime string, data []byte) ports.Snapshot {
	return ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: mime, Data: data}},
	}}
}

// waitClipboardImage waits until the clipboard holds an image of the given size.
func waitClipboardImage(t *testing.T, clipboard *fake.Clipboard, width, height int) []byte {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if content, ok := clipboard.LastWrite(); ok {
			for _, rep := range content.Representations {
				if rep.MIME != clip.MIMEImagePNG {
					continue
				}
				decoded, err := png.Decode(bytes.NewReader(rep.Data))
				if err != nil {
					continue
				}
				if decoded.Bounds() == image.Rect(0, 0, width, height) {
					return rep.Data
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the clipboard did not receive a %dx%d image within %v", width, height, testTimeout)
	return nil
}

// TestNodesClipImage moves an image from one device to the other.
func TestNodesClipImage(t *testing.T) {
	a, b := clipNodes(t, 40000)
	events := b.subscribe(t)

	data := pngBytes(t, 24, 18)
	a.clipboard.Emit(imageSnapshot(clip.MIMEImagePNG, data))
	waitClipboardImage(t, b.clipboard, 24, 18)

	event := waitEvent(t, events, EventClipboard, a.DeviceID())
	if event.Local {
		t.Fatal("an incoming item is marked as local")
	}
	if event.Item.Kind != model.KindImage {
		t.Fatalf("item kind is %s, want image", event.Item.Kind)
	}
	if len(event.Item.Representations) == 0 || event.Item.Representations[0].MIME != clip.MIMEImagePNG {
		t.Fatal("the applied item carries no png representation")
	}
}

// TestNodesClipBlobImage checks that an image above the inline limit arrives.
// The payload travels through the blob store, so the announcement handler must
// not fetch it while it holds the control stream of the session.
func TestNodesClipBlobImage(t *testing.T) {
	a, b := clipNodes(t, 54000)
	events := b.subscribe(t)

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, noisyImage(320, 240)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	data := buffer.Bytes()
	if len(data) <= clip.MaxInlineText {
		t.Fatalf("the test image is %d bytes, want more than %d", len(data), clip.MaxInlineText)
	}

	a.clipboard.Emit(imageSnapshot(clip.MIMEImagePNG, data))
	if applied := waitClipboardImage(t, b.clipboard, 320, 240); !bytes.Equal(applied, data) {
		t.Fatal("the applied image differs from the announced one")
	}

	event := waitEvent(t, events, EventClipboard, a.DeviceID())
	if event.Item.Kind != model.KindImage {
		t.Fatalf("item kind is %s, want image", event.Item.Kind)
	}
}

// TestNodesClipTIFF checks that a TIFF image arrives as PNG.
func TestNodesClipTIFF(t *testing.T) {
	a, b := clipNodes(t, 42000)
	events := b.subscribe(t)

	a.clipboard.Emit(imageSnapshot("image/tiff", tiffBytes(t, 20, 12)))
	pngData := waitClipboardImage(t, b.clipboard, 20, 12)

	event := waitEvent(t, events, EventClipboard, a.DeviceID())
	reps := event.Item.Representations
	if len(reps) == 0 || reps[0].MIME != clip.MIMEImagePNG {
		t.Fatalf("the item holds %d representations", len(reps))
	}
	if reps[0].Hash != blake3.Sum256(pngData) {
		t.Fatal("the png of the item differs from the applied image")
	}
}

// TestNodesClipFileKeepsMetadata checks that a file above the inline limit
// travels as metadata and is fetched on request.
func TestNodesClipFileKeepsMetadata(t *testing.T) {
	a, b := clipNodes(t, 44000)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	data := bytes.Repeat([]byte("unison file content "), 12*1024)
	path := filepath.Join(t.TempDir(), "report.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	item, err := a.Clip().Submit(ctx, ports.Content{Representations: []ports.Representation{
		{MIME: clip.MIMEUnisonFile, FilePath: path},
	}})
	if err != nil {
		t.Fatalf("submit file: %v", err)
	}

	event := waitEvent(t, events, EventClipDownload, a.DeviceID())
	if event.Item.ID != item.ID {
		t.Fatalf("event is for %s, want %s", event.Item.ID, item.ID)
	}
	if content, ok := b.clipboard.LastWrite(); ok {
		t.Fatalf("a metadata item reached the clipboard: %v", content)
	}

	stored, err := b.History().Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("read the history item: %v", err)
	}
	if stored.Complete {
		t.Fatal("the payload of the item is reported as stored")
	}
	if len(stored.Representations) != 1 {
		t.Fatalf("got %d representations, want one", len(stored.Representations))
	}
	rep := stored.Representations[0]
	if rep.MIME != clip.MIMEUnisonFile || rep.FileName != "report.bin" {
		t.Fatalf("representation is %s %q", rep.MIME, rep.FileName)
	}
	if rep.Size != uint64(len(data)) || len(rep.Inline) != 0 {
		t.Fatalf("representation is %d bytes with %d bytes inline", rep.Size, len(rep.Inline))
	}
	if rep.MTime.IsZero() {
		t.Fatal("the file modification time is lost")
	}

	dir := t.TempDir()
	downloaded, paths, err := b.History().Fetch(ctx, item.ID, dir)
	if err != nil {
		t.Fatalf("fetch the payload: %v", err)
	}
	if !downloaded.Complete {
		t.Fatal("the fetched item is incomplete")
	}
	if len(paths) != 1 {
		t.Fatalf("fetch wrote %d paths", len(paths))
	}
	saved, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read the downloaded file: %v", err)
	}
	if !bytes.Equal(saved, data) {
		t.Fatal("the downloaded file differs from the original")
	}
}

// TestNodesClipFileBelowLimit checks that a small file travels inline.
func TestNodesClipFileBelowLimit(t *testing.T) {
	a, b := clipNodes(t, 46000)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	data := []byte("a small attachment")
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	item, err := a.Clip().Submit(ctx, ports.Content{Representations: []ports.Representation{
		{MIME: clip.MIMEUnisonFile, FilePath: path},
	}})
	if err != nil {
		t.Fatalf("submit file: %v", err)
	}

	waitEvent(t, events, EventClipDownload, a.DeviceID())
	stored, err := b.History().Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("read the history item: %v", err)
	}
	if len(stored.Representations) != 1 || !bytes.Equal(stored.Representations[0].Inline, data) {
		t.Fatal("the small file does not travel inline")
	}
}

// TestNodesClipConfirm checks that a peer with the confirmation flag produces a
// pending item.
func TestNodesClipConfirm(t *testing.T) {
	a, b := clipNodes(t, 48000)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.Trust().SetRequireConfirmation(ctx, a.DeviceID(), true); err != nil {
		t.Fatalf("require confirmation: %v", err)
	}

	a.clipboard.Emit(textSnapshot("confirm me"))
	event := waitEvent(t, events, EventClipApply, a.DeviceID())
	if event.Item.Kind != model.KindText {
		t.Fatalf("item kind is %s", event.Item.Kind)
	}
	if _, ok := b.clipboard.LastWrite(); ok {
		t.Fatal("the item reached the clipboard without confirmation")
	}
	if _, err := b.History().Get(ctx, event.Item.ID); err != nil {
		t.Fatalf("the pending item is not in the history: %v", err)
	}
}
