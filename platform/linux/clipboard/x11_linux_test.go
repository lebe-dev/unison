//go:build linux && !x11clipboardtest

package clipboard

import (
	"errors"
	"testing"
)

func TestNewX11NoDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	clipboard, err := NewX11(X11Options{})
	if !errors.Is(err, ErrNoDisplay) {
		t.Fatalf("NewX11 error = %v, want ErrNoDisplay", err)
	}
	if clipboard != nil {
		t.Fatalf("NewX11 returned %v, want nil", clipboard)
	}
}

func TestNewX11ExplicitDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	clipboard, err := NewX11(X11Options{Display: ":99"})
	if err != nil {
		t.Fatalf("NewX11: %v", err)
	}
	if clipboard.display != ":99" {
		t.Fatalf("display = %q, want %q", clipboard.display, ":99")
	}
}
