package clipboard

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// touch creates an empty file and returns its path.
func touch(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
	return path
}

func TestPickFiles(t *testing.T) {
	cases := []struct {
		name    string
		targets targetList
		want    string
	}{
		{name: "nautilus first", targets: targetList{MIMEURIList, MIMEGNOMECopiedFiles}, want: MIMEGNOMECopiedFiles},
		{name: "uri list", targets: targetList{atomUTF8String, MIMEURIList}, want: MIMEURIList},
		{name: "text only", targets: targetList{atomUTF8String}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.targets.pickFiles(); got != tc.want {
				t.Fatalf("pickFiles() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseGNOMECopiedFiles(t *testing.T) {
	dir := t.TempDir()
	first := touch(t, dir, "first.txt")
	second := touch(t, dir, "second file.txt")
	payload := "copy\n" + pathToURI(first) + "\r\n" + pathToURI(second) + "\n"

	got := filePaths(parseGNOMECopiedFiles([]byte(payload)))
	want := []string{first, second}
	assertPaths(t, got, want)
}

func TestParseGNOMECopiedFilesCutIsCopy(t *testing.T) {
	dir := t.TempDir()
	path := touch(t, dir, "a.txt")
	payload := "cut\n" + pathToURI(path) + "\n"

	assertPaths(t, filePaths(parseGNOMECopiedFiles([]byte(payload))), []string{path})
}

func TestParseGNOMECopiedFilesWithoutHeader(t *testing.T) {
	dir := t.TempDir()
	path := touch(t, dir, "a.txt")

	assertPaths(t, filePaths(parseGNOMECopiedFiles([]byte(pathToURI(path)))), []string{path})
}

func TestParseURIList(t *testing.T) {
	dir := t.TempDir()
	first := touch(t, dir, "one.txt")
	second := touch(t, dir, "два.txt")
	payload := "# copied by a file manager\r\n" +
		pathToURI(first) + "\r\n" +
		"\r\n" +
		pathToURI(second) + "\r\n"

	assertPaths(t, filePaths(parseURIList([]byte(payload))), []string{first, second})
}

func TestParseURIListSkipsForeignAndBroken(t *testing.T) {
	dir := t.TempDir()
	local := touch(t, dir, "local.txt")
	payload := "https://example.com/a\n" +
		"file://remote-host/share/a.txt\n" +
		"file:///nonexistent/уникод.txt\n" +
		"file:///tmp/%zz\n" +
		"file://localhost" + local + "\n" +
		"file://" + local + "\n"

	assertPaths(t, filePaths(parseURIList([]byte(payload))), []string{local})
}

func TestParseURIListLocalhost(t *testing.T) {
	dir := t.TempDir()
	path := touch(t, dir, "host.txt")

	assertPaths(t, filePaths(parseURIList([]byte("file://localhost"+path+"/"))), nil)
	assertPaths(t, filePaths(parseURIList([]byte("file://localhost"+path))), []string{path})
}

func TestParseURIListDropsDirectories(t *testing.T) {
	dir := t.TempDir()

	assertPaths(t, filePaths(parseURIList([]byte(pathToURI(dir)+"\n"))), nil)
}

func TestParseURIListSpacesAndCyrillic(t *testing.T) {
	dir := t.TempDir()
	name := "файл с пробелом.txt"
	path := touch(t, dir, name)

	uri := pathToURI(path)
	if want := "file://" + filepath.ToSlash(dir) + "/" + "%D1%84%D0%B0%D0%B9%D0%BB%20%D1%81%20%D0%BF%D1%80%D0%BE%D0%B1%D0%B5%D0%BB%D0%BE%D0%BC.txt"; uri != want {
		t.Fatalf("pathToURI() = %q, want %q", uri, want)
	}
	assertPaths(t, filePaths(parseURIList([]byte(uri))), []string{path})

	assertPaths(t, filePaths(parseURIList([]byte("file://"+filepath.ToSlash(dir)+"/%D1%84%D0%B0%D0%B9%D0%BB%20%D1%81%20%D0%BF%D1%80%D0%BE%D0%B1%D0%B5%D0%BB%D0%BE%D0%BC.txt"))), []string{path})
}

func TestParseURIListDeduplicates(t *testing.T) {
	dir := t.TempDir()
	path := touch(t, dir, "a.txt")
	payload := pathToURI(path) + "\n" + pathToURI(path) + "\n"

	assertPaths(t, filePaths(parseURIList([]byte(payload))), []string{path})
}

func TestParseURIListLimits(t *testing.T) {
	dir := t.TempDir()
	payload := make([]byte, 0)
	for i := range maxFiles + 10 {
		path := touch(t, dir, fmt.Sprintf("file%03d.txt", i))
		payload = append(payload, []byte(pathToURI(path)+"\n")...)
	}

	if got := filePaths(parseURIList(payload)); len(got) != maxFiles {
		t.Fatalf("filePaths returned %d paths, want %d", len(got), maxFiles)
	}
}

func TestEncodeURIList(t *testing.T) {
	dir := t.TempDir()
	first := touch(t, dir, "a b.txt")
	second := touch(t, dir, "кириллица.txt")

	got := string(encodeURIList([]string{first, second}))
	want := pathToURI(first) + "\r\n" + pathToURI(second) + "\r\n"
	if got != want {
		t.Fatalf("encodeURIList() = %q, want %q", got, want)
	}
}

func TestEncodeGNOMECopiedFiles(t *testing.T) {
	dir := t.TempDir()
	path := touch(t, dir, "a.txt")

	got := string(encodeGNOMECopiedFiles([]string{path}))
	want := "copy\n" + pathToURI(path) + "\n"
	if got != want {
		t.Fatalf("encodeGNOMECopiedFiles() = %q, want %q", got, want)
	}
}

func TestEncodePlainPaths(t *testing.T) {
	got := string(encodePlainPaths([]string{"/tmp/a", "/tmp/b"}))
	if want := "/tmp/a\n/tmp/b\n"; got != want {
		t.Fatalf("encodePlainPaths() = %q, want %q", got, want)
	}
}

func TestFileURIFailure(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "relative", path: "a.txt"},
		{name: "empty", path: ""},
		{name: "nul", path: "/tmp/a\x00b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := checkedPaths([]string{tc.path}); err == nil {
				t.Fatalf("checkedPaths(%q) = nil error, want an error", tc.path)
			}
		})
	}
}

func TestCheckedPathsTooMany(t *testing.T) {
	paths := make([]string, maxFiles+1)
	for i := range paths {
		paths[i] = fmt.Sprintf("/tmp/file%03d", i)
	}
	if _, err := checkedPaths(paths); err == nil {
		t.Fatal("checkedPaths of too many files = nil error, want an error")
	}
}

// assertPaths compares path lists.
func assertPaths(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}
