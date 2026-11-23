//go:build windows

package daemon

import (
	"strings"
	"testing"
)

// TestNewClipboardModes checks the clipboard modes of the Windows daemon.
func TestNewClipboardModes(t *testing.T) {
	for _, mode := range []string{"", ClipboardSystem, ClipboardAuto} {
		clip, err := NewClipboard(mode)
		if err != nil {
			t.Fatalf("NewClipboard(%q): %v", mode, err)
		}
		if clip == nil {
			t.Fatalf("NewClipboard(%q) returned no clipboard", mode)
		}
	}

	clip, err := NewClipboard(ClipboardNone)
	if err != nil {
		t.Fatalf("NewClipboard(%q): %v", ClipboardNone, err)
	}
	if _, ok := clip.(noClipboard); !ok {
		t.Fatalf("NewClipboard(%q) returned %T, want the empty clipboard", ClipboardNone, clip)
	}
}

// TestNewClipboardRejectsLinuxModes checks that the modes of the Linux backends
// name the platform in their error.
func TestNewClipboardRejectsLinuxModes(t *testing.T) {
	for _, mode := range []string{ClipboardX11, ClipboardWayland, "unknown"} {
		_, err := NewClipboard(mode)
		if err == nil {
			t.Fatalf("NewClipboard(%q) returned no error", mode)
		}
		if !strings.Contains(err.Error(), "not available on Windows") {
			t.Errorf("NewClipboard(%q) = %v, want an error about Windows", mode, err)
		}
	}
}

// TestClipboardStatus checks the backend the daemon reports for the clipboard.
func TestClipboardStatus(t *testing.T) {
	clip, err := NewClipboard(ClipboardSystem)
	if err != nil {
		t.Fatalf("NewClipboard: %v", err)
	}
	if backend, note := clipboardStatus(clip); backend != "win32" || note != "" {
		t.Errorf("clipboardStatus = %q, %q; want win32 and no note", backend, note)
	}

	backend, note := clipboardStatus(NoClipboard())
	if backend != ClipboardNone || note == "" {
		t.Errorf("clipboardStatus of the empty clipboard = %q, %q; want %q and a note", backend, note, ClipboardNone)
	}
}
