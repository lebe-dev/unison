package keystorename

import (
	"strings"
	"testing"
)

// TestServiceOfTheDefaultDirectory checks that the default data directory keeps
// the shared name.
func TestServiceOfTheDefaultDirectory(t *testing.T) {
	for _, variant := range []string{"/home/u/.unison", "/home/u/.unison/", "/home/u/./.unison"} {
		if got := Service(variant, "/home/u/.unison"); got != DefaultService {
			t.Errorf("Service(%q) = %q, want %q", variant, got, DefaultService)
		}
	}
}

// TestServiceWithoutDefaultDirectory names a directory when no directory is the
// default one.
func TestServiceWithoutDefaultDirectory(t *testing.T) {
	got := Service("/home/u/.unison", "")
	if !strings.HasPrefix(got, DefaultService+".dev.") {
		t.Errorf("Service = %q, want a name of its own", got)
	}
}

// TestServiceNames checks the name of a directory that is not the default one.
func TestServiceNames(t *testing.T) {
	cases := []struct{ name, dir, want string }{
		{"readable name", "/tmp/Unison-2", DefaultService + ".dev.unison-2-" + ShortHash("/tmp/Unison-2")},
		{"lower case", "/tmp/unisond2", DefaultService + ".dev.unisond2-" + ShortHash("/tmp/unisond2")},
		{"sanitized", "/tmp/my dir", DefaultService + ".dev.my-dir-" + ShortHash("/tmp/my dir")},
		{"no usable name", "/", DefaultService + ".dev.dir-" + ShortHash("/")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Service(tc.dir, "/home/u/.unison"); got != tc.want {
				t.Errorf("Service(%q) = %q, want %q", tc.dir, got, tc.want)
			}
		})
	}
}

// TestServiceIsStable checks that one directory maps to the same name whatever
// the spelling of its path.
func TestServiceIsStable(t *testing.T) {
	dir := "/tmp/Unison-2"
	want := Service(dir, "/home/u/.unison")
	for _, variant := range []string{dir, dir + "/", "/tmp/./Unison-2", "/tmp/work/../Unison-2"} {
		if got := Service(variant, "/home/u/.unison"); got != want {
			t.Errorf("Service(%q) = %q, want %q", variant, got, want)
		}
	}
}

// TestServiceTellsDirectoriesApart checks that two directories with the same
// name keep their own secrets.
func TestServiceTellsDirectoriesApart(t *testing.T) {
	first := Service("/Users/someone/work/unison", "/home/u/.unison")
	second := Service("/Users/someone/test/unison", "/home/u/.unison")
	if first == second {
		t.Fatalf("both directories map to %q", first)
	}
	together := Service("/tmp/my dir", "/home/u/.unison")
	same := Service("/tmp/my-dir", "/home/u/.unison")
	if together == same {
		t.Fatalf("two slugs that read the same map to %q", together)
	}
}

// TestSlug checks the readable part of a name.
func TestSlug(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"separator", "Unison-2", "unison-2"},
		{"spaces", "My Data Dir", "my-data-dir"},
		{"punctuation", "app.unison (v2)", "app-unison-v2"},
		{"leading and trailing separators", "--x--", "x"},
		{"only separators", "!!!", ""},
		{"long", strings.Repeat("a", 40), strings.Repeat("a", SlugLimit)},
		{"cut at a separator", strings.Repeat("a", 23) + "-b", strings.Repeat("a", 23)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Slug(tc.in); got != tc.want {
				t.Errorf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestShortHash checks the length of the hash and that a trailing separator does
// not change it.
func TestShortHash(t *testing.T) {
	hash := ShortHash("/tmp/Unison-2")
	if len(hash) != 8 {
		t.Errorf("ShortHash = %q, want four bytes", hash)
	}
	if got := ShortHash("/tmp/Unison-2/"); got != hash {
		t.Errorf("ShortHash of the same directory = %q, want %q", got, hash)
	}
	if got := ShortHash("/tmp/Unison-3"); got == hash {
		t.Errorf("two directories share the hash %q", hash)
	}
}
