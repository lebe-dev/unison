//go:build !linux

package clipboard

import (
	"errors"
	"testing"
)

func TestNewX11Unsupported(t *testing.T) {
	clipboard, err := NewX11(X11Options{Display: ":0"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("NewX11 error = %v, want ErrUnsupported", err)
	}
	if clipboard != nil {
		t.Fatalf("NewX11 returned %v, want nil", clipboard)
	}
}
