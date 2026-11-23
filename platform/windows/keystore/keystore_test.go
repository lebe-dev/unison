package keystore

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewRejectsEmptyDir(t *testing.T) {
	if _, err := New("", nil); err == nil {
		t.Fatal("New(\"\") returned no error")
	}
}

func TestNewKeepsDir(t *testing.T) {
	dir := filepath.Join("a", "..", "keys")
	k, err := New(dir, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := k.Dir(), "keys"; got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

func TestNewCopiesEntropy(t *testing.T) {
	entropy := []byte("entropy")
	k, err := New(t.TempDir(), entropy)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entropy[0] = 'E'
	if k.entropy[0] != 'e' {
		t.Fatal("New kept the caller slice instead of a copy")
	}
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join("C:", "Users", "test", "AppData", "Local"))
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	want := filepath.Join("C:", "Users", "test", "AppData", "Local", "Unison", "keys")
	if got != want {
		t.Fatalf("DefaultDir() = %q, want %q", got, want)
	}
}

func TestDefaultDirWithoutLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	if _, err := DefaultDir(); err == nil {
		t.Fatal("DefaultDir without LOCALAPPDATA returned no error")
	}
}

func TestPath(t *testing.T) {
	dir := t.TempDir()
	k, err := New(dir, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tests := []struct {
		name string
		want string
	}{
		{name: "store.dek", want: filepath.Join(dir, "store.dek.bin")},
		{name: "identity/ed25519-seed", want: filepath.Join(dir, "identity", "ed25519-seed.bin")},
		{name: "a/b/c-seed", want: filepath.Join(dir, "a", "b", "c-seed.bin")},
	}
	for _, tt := range tests {
		got, err := k.path(tt.name)
		if err != nil {
			t.Fatalf("path(%q): %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("path(%q) = %q, want %q", tt.name, got, tt.want)
		}
		if !strings.HasPrefix(got, dir+string(filepath.Separator)) {
			t.Fatalf("path(%q) = %q, outside %q", tt.name, got, dir)
		}
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{
		"store.dek",
		"identity/ed25519-seed",
		"identity/mldsa65-seed",
		"a/b/c",
		"name with space",
		"under_score-1.2",
	}
	for _, name := range valid {
		if err := validateName(name); err != nil {
			t.Errorf("validateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"",
		"/abs",
		"a/",
		"a//b",
		".",
		"..",
		"../escape",
		"a/../b",
		"a/./b",
		`a\b`,
		`..\escape`,
		"C:/abs",
		"a:b",
		"a*b",
		"a?b",
		`a"b`,
		"a<b",
		"a>b",
		"a|b",
		"trailing.",
		"trailing ",
		"tab\tname",
		"nul\x00byte",
		"con",
		"CON.bin",
		"com1",
		"lpt9.ext",
		strings.Repeat("a", maxElemLen),
	}
	for _, name := range invalid {
		err := validateName(name)
		if err == nil {
			t.Errorf("validateName(%q) returned no error", name)
			continue
		}
		if !errors.Is(err, errInvalidName) {
			t.Errorf("validateName(%q) = %v, want errInvalidName", name, err)
		}
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "store.dek.bin")

	if err := writeFile(path, []byte("first")); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("content = %q, want %q", got, "first")
	}

	if err := writeFile(path, []byte("second")); err != nil {
		t.Fatalf("writeFile over existing file: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("directory holds %v, want only the target file", entries)
	}
}

func TestWriteFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports fixed permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "keys", "store.dek.bin")
	if err := writeFile(path, []byte("secret")); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
}

func TestWriteFileReportsError(t *testing.T) {
	dir := t.TempDir()
	// A file where a directory is expected makes MkdirAll fail.
	blocker := filepath.Join(dir, "keys")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := writeFile(filepath.Join(blocker, "store.dek.bin"), []byte("secret")); err == nil {
		t.Fatal("writeFile below a regular file returned no error")
	}
}
