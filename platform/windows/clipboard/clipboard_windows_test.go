//go:build windows

package clipboard

import (
	"context"
	"errors"
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
		{Representations: []ports.Representation{{MIME: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}}}},
	}
	for _, content := range tests {
		if err := c.Write(context.Background(), content); !errors.Is(err, ErrUnsupportedContent) {
			t.Fatalf("Write error = %v, want ErrUnsupportedContent", err)
		}
	}
}
