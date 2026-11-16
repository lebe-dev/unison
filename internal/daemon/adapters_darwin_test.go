//go:build darwin

package daemon

import (
	"strings"
	"testing"
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
		{"separator", "/tmp/Unison-2", "app.unison.dev.unison-2"},
		{"lower case", "/tmp/unisond2", "app.unison.dev.unisond2"},
		{"upper case", "/tmp/Work", "app.unison.dev.work"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keychainService(tc.dir); got != tc.want {
				t.Errorf("keychainService(%q) = %q, want %q", tc.dir, got, tc.want)
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
// carries a suffix of its path, so two such directories stay apart.
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

// TestKeychainServiceWithoutName checks a data directory that has no usable
// name.
func TestKeychainServiceWithoutName(t *testing.T) {
	got := keychainService("/")
	if !strings.HasPrefix(got, "app.unison.dev.dir-") {
		t.Errorf("keychainService(%q) = %q", "/", got)
	}
}

// TestServiceSlug checks the label of a directory name.
func TestServiceSlug(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"separator", "Unison-2", "unison-2"},
		{"spaces", "My Data Dir", "my-data-dir"},
		{"punctuation", "app.unison (v2)", "app-unison-v2"},
		{"leading and trailing separators", "--x--", "x"},
		{"only separators", "!!!", ""},
		{"long", strings.Repeat("a", 40), strings.Repeat("a", serviceSlugLimit)},
		{"cut at a separator", strings.Repeat("a", 23) + "-b", strings.Repeat("a", 23)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serviceSlug(tc.in); got != tc.want {
				t.Errorf("serviceSlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
