//go:build darwin

package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/platform/darwin/clipboard"
)

// TestKeychainServiceOfDefaultDataDir checks that the default data directory
// keeps the shared service.
func TestKeychainServiceOfDefaultDataDir(t *testing.T) {
	dir, err := DefaultDataDir()
	if err != nil {
		t.Fatalf("default data directory: %v", err)
	}
	for _, variant := range []string{dir, dir + "/", dir + "/."} {
		if got := keychainService(variant); got != "app.unison" {
			t.Errorf("keychainService(%q) = %q, want app.unison", variant, got)
		}
	}
}

// TestKeychainServiceOfDataDir checks the readable name of a data directory of
// its own.
func TestKeychainServiceOfDataDir(t *testing.T) {
	cases := []struct{ name, dir, want string }{
		{"separator", "/tmp/Unison-2", "app.unison.dev.unison-2-"},
		{"lower case", "/tmp/unisond2", "app.unison.dev.unisond2-"},
		{"upper case", "/tmp/Work", "app.unison.dev.work-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := keychainService(tc.dir)
			if !strings.HasPrefix(got, tc.want) {
				t.Errorf("keychainService(%q) = %q, want the prefix %q", tc.dir, got, tc.want)
			}
		})
	}
}

// TestKeychainServiceIsStable checks that one directory always maps to the same
// service, whatever the spelling of its path.
func TestKeychainServiceIsStable(t *testing.T) {
	dir := "/tmp/Unison-2"
	want := keychainService(dir)
	variants := []string{dir, dir + "/", "/tmp/./Unison-2", "/tmp/work/../Unison-2"}
	for _, variant := range variants {
		if got := keychainService(variant); got != want {
			t.Errorf("keychainService(%q) = %q, want %q", variant, got, want)
		}
	}
}

// TestKeychainServiceOfSanitizedNames checks that a name which needs sanitizing
// keeps its directory apart from every other one.
func TestKeychainServiceOfSanitizedNames(t *testing.T) {
	first := keychainService("/tmp/a/my dir")
	if !strings.HasPrefix(first, "app.unison.dev.my-dir-") {
		t.Errorf("keychainService of a sanitized name = %q", first)
	}
	second := keychainService("/tmp/b/my dir")
	if first == second {
		t.Errorf("two directories share the service %q", first)
	}
	if got := keychainService("/tmp/a/my dir"); got != first {
		t.Errorf("the service of a directory changed from %q to %q", first, got)
	}
}

// TestKeychainServiceOfSameNamedDirectories checks that two data directories
// with the same name keep their keys apart.
func TestKeychainServiceOfSameNamedDirectories(t *testing.T) {
	first := keychainService("/tmp/a/work")
	second := keychainService("/tmp/b/work")
	if first == second {
		t.Fatalf("two data directories share the service %q", first)
	}
}

// TestKeychainServiceWithoutName checks a data directory that has no usable
// name.
func TestKeychainServiceWithoutName(t *testing.T) {
	got := keychainService("/")
	if !strings.HasPrefix(got, "app.unison.dev.dir-") {
		t.Errorf("keychainService(%q) = %q", "/", got)
	}
}

// TestStartupWarning checks that the daemon warns about every pasteboard
// policy that keeps it from reading the clipboard, and stays quiet otherwise.
func TestStartupWarning(t *testing.T) {
	cases := []struct {
		behavior clipboard.Behavior
		warns    bool
	}{
		{clipboard.BehaviorDefault, true},
		{clipboard.BehaviorAsk, true},
		{clipboard.BehaviorAlwaysDeny, true},
		{clipboard.BehaviorAlwaysAllow, false},
		{behaviorUnknown, false},
	}
	for _, tc := range cases {
		warning := (&policyClipboard{behavior: tc.behavior}).StartupWarning(context.Background())
		if got := warning != ""; got != tc.warns {
			t.Errorf("behavior %d: warning %q, want a warning: %v", tc.behavior, warning, tc.warns)
		}
		if tc.warns && !strings.Contains(warning, "Paste from Other Apps") {
			t.Errorf("behavior %d: warning %q does not name the setting", tc.behavior, warning)
		}
	}
}
