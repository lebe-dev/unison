//go:build linux && !x11clipboardtest

package clipboard

import (
	"errors"
	"testing"
	"time"
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

// TestExpireTransfers drops the transfers whose requestor stopped reading.
func TestExpireTransfers(t *testing.T) {
	abandoned := incrKey{requestor: 7, property: 9}
	live := incrKey{requestor: 1, property: 2}
	srv := &server{transfers: map[incrKey]*incrTransfer{
		abandoned: {data: []byte("value"), active: time.Now().Add(-2 * maxTransferTime)},
		live:      {data: []byte("value"), active: time.Now()},
	}}

	srv.expireTransfers()

	if _, ok := srv.transfers[abandoned]; ok {
		t.Fatal("the abandoned transfer is still held")
	}
	if _, ok := srv.transfers[live]; !ok {
		t.Fatal("the running transfer was dropped")
	}
}
