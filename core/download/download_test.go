package download

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "report.txt", want: "report.txt"},
		{name: "traversal", in: "../../etc/passwd", want: "passwd"},
		{name: "absolute", in: "/etc/passwd", want: "passwd"},
		{name: "backslash", in: `..\..\windows\system32\hosts`, want: "hosts"},
		{name: "root", in: "/", want: "file"},
		{name: "control", in: "a\x00b.txt", want: "a-b.txt"},
		{name: "empty", in: "", want: "file"},
		{name: "dots", in: "..", want: "file"},
		{name: "trailing dot", in: "name.", want: "name"},
		{name: "trailing space", in: "name ", want: "name"},
		{name: "reserved", in: "CON", want: "_CON"},
		{name: "reserved with extension", in: "nul.txt", want: "_nul.txt"},
		{name: "reserved inside", in: "console.txt", want: "console.txt"},
		{name: "right to left override", in: "photo\u202egnp.command", want: "photognp.command"},
		{name: "isolate", in: "a\u2066b\u2069.txt", want: "ab.txt"},
		{name: "zero width", in: "in\u200bvoice.pdf", want: "invoice.pdf"},
		{name: "alternate data stream", in: "file.txt:hidden", want: "file.txt-hidden"},
		{name: "windows punctuation", in: `a<b>c"d|e?f*g.txt`, want: "a-b-c-d-e-f-g.txt"},
		{name: "only format characters", in: "\u202e\u200b", want: "file"},
		{name: "unicode letters", in: "отчёт.pdf", want: "отчёт.pdf"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SafeName(test.in); got != test.want {
				t.Fatalf("SafeName(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestSafeNameLength(t *testing.T) {
	name := SafeName(strings.Repeat("я", MaxNameLength) + ".bin")
	if len(name) > MaxNameLength {
		t.Fatalf("name of %d bytes, limit is %d", len(name), MaxNameLength)
	}
	if !strings.HasPrefix(name, "я") || strings.ContainsRune(name, '�') {
		t.Fatalf("the name was cut inside a character: %q", name)
	}
}

func TestNumberedName(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		want    string
	}{
		{name: "photo.jpg", attempt: 0, want: "photo.jpg"},
		{name: "photo.jpg", attempt: 1, want: "photo (1).jpg"},
		{name: "photo.jpg", attempt: 12, want: "photo (12).jpg"},
		{name: "archive.tar.gz", attempt: 1, want: "archive.tar (1).gz"},
		{name: "README", attempt: 2, want: "README (2)"},
	}
	for _, test := range tests {
		if got := numberedName(test.name, test.attempt); got != test.want {
			t.Fatalf("numberedName(%q, %d) = %q, want %q", test.name, test.attempt, got, test.want)
		}
	}
}

// TestCreateNeverReplacesAFile checks that a received name never overwrites a
// file of the user.
func TestCreateNeverReplacesAFile(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(existing, []byte("mine"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, file, err := Create(dir, "notes.txt")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = file.Close()

	if path != filepath.Join(dir, "notes (1).txt") {
		t.Fatalf("created %s", path)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "mine" {
		t.Fatalf("the existing file changed: %q, %v", data, err)
	}
}

// TestCreateStaysInTheDirectory checks the sanitizing of the name on create.
func TestCreateStaysInTheDirectory(t *testing.T) {
	dir := t.TempDir()
	path, file, err := Create(dir, "../escape.txt")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = file.Close()
	if filepath.Dir(path) != dir {
		t.Fatalf("the file %s left the directory %s", path, dir)
	}
}

// TestCreateMarksTheFile checks that a created file carries the download mark
// of the platform.
func TestCreateMarksTheFile(t *testing.T) {
	dir := t.TempDir()
	path, file, err := Create(dir, "tool.command")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = file.Close()

	marked, err := isMarked(path)
	if err != nil {
		t.Fatalf("read the mark: %v", err)
	}
	if marked != marksFiles {
		t.Fatalf("marked is %v, want %v on this platform", marked, marksFiles)
	}
}
