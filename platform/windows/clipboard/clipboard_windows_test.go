//go:build windows

package clipboard

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
	"unsafe"

	"github.com/lebe-dev/unison/core/ports"
)

// The Win32 structures are mirrored by hand, so their layout is checked against
// the documented sizes.
func TestStructLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("the adapter targets 64 bit Windows")
	}
	if got, want := unsafe.Sizeof(msg{}), uintptr(48); got != want {
		t.Fatalf("msg is %d bytes, Win32 MSG is %d", got, want)
	}
	if got, want := unsafe.Sizeof(wndClassEx{}), uintptr(80); got != want {
		t.Fatalf("wndClassEx is %d bytes, Win32 WNDCLASSEXW is %d", got, want)
	}
}

func TestRegisteredFormats(t *testing.T) {
	ids := registeredFormats()
	tests := map[string]uint32{
		formatPNG:     ids.png,
		formatHTML:    ids.html,
		MarkerFormat:  ids.origin,
		formatExclude: ids.exclude,
		formatHistory: ids.history,
		formatCloud:   ids.cloud,
	}
	seen := make(map[uint32]string, len(tests))
	for name, id := range tests {
		if id == 0 {
			t.Fatalf("format %q was not registered", name)
		}
		if other, ok := seen[id]; ok {
			t.Fatalf("formats %q and %q share the identifier %d", name, other, id)
		}
		seen[id] = name
	}
}

func TestWriteRejectsUnsupportedContent(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tests := []ports.Content{
		{},
		{Representations: []ports.Representation{{MIME: "image/gif", Data: []byte("GIF89a")}}},
	}
	for _, content := range tests {
		if err := c.Write(context.Background(), content); !errors.Is(err, ErrUnsupportedContent) {
			t.Fatalf("Write error = %v, want ErrUnsupportedContent", err)
		}
		if err := c.WriteExcluded(context.Background(), content); !errors.Is(err, ErrUnsupportedContent) {
			t.Fatalf("WriteExcluded error = %v, want ErrUnsupportedContent", err)
		}
	}
}

func TestPrepareImage(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 0xFF, A: 0xFF})
	source.SetNRGBA(1, 0, color.NRGBA{B: 0xFF, A: 0xFF})
	encoded, err := encodePNG(source)
	if err != nil {
		t.Fatalf("encodePNG: %v", err)
	}

	prepared, err := prepare(ports.Content{Representations: []ports.Representation{
		{MIME: MIMEImagePNG, Data: encoded},
	}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !bytes.Equal(prepared.png, encoded) {
		t.Fatal("prepare did not keep the PNG payload")
	}
	decoded, err := decodeDIB(prepared.dib)
	if err != nil {
		t.Fatalf("decodeDIB: %v", err)
	}
	if got := decoded.Bounds(); got != image.Rect(0, 0, 2, 1) {
		t.Fatalf("bitmap bounds = %v, want 2x1", got)
	}
	if got := decoded.NRGBAAt(0, 0); got != (color.NRGBA{R: 0xFF, A: 0xFF}) {
		t.Fatalf("first bitmap pixel = %+v, want red", got)
	}
}

func TestPrepareRejectsBrokenImage(t *testing.T) {
	_, err := prepare(ports.Content{Representations: []ports.Representation{
		{MIME: MIMEImagePNG, Data: []byte("not a png")},
	}})
	if !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("prepare error = %v, want ErrUnsupportedContent", err)
	}
}

func TestPrepareFiles(t *testing.T) {
	prepared, err := prepare(ports.Content{Representations: []ports.Representation{
		{MIME: "application/x-unison-file", FilePath: `C:\Users\a\report.pdf`},
	}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	paths, err := decodeDropFiles(prepared.dropFiles)
	if err != nil {
		t.Fatalf("decodeDropFiles: %v", err)
	}
	if len(paths) != 1 || paths[0] != `C:\Users\a\report.pdf` {
		t.Fatalf("prepared paths = %v", paths)
	}
}
