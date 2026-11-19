package store

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/lebe-dev/unison/core/ports/fake"
)

func TestFileURIParts(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		host  string
		parts string
	}{
		{"drive", `C:\Users\duket\AppData\Local\Unison\unison.db`, "", "/C:/Users/duket/AppData/Local/Unison/unison.db"},
		{"space and percent", `D:\dir with space\100%\unison.db`, "", "/D:/dir with space/100%/unison.db"},
		{"cyrillic", `C:\Пользователи\Данные\unison.db`, "", "/C:/Пользователи/Данные/unison.db"},
		{"forward slashes", "C:/Users/duket/unison.db", "", "/C:/Users/duket/unison.db"},
		{"unc", `\\server\share\dir with space\unison.db`, "server", "/share/dir with space/unison.db"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, parts := windowsURIParts(test.path)
			if host != test.host || parts != test.parts {
				t.Fatalf("windowsURIParts(%q) = %q, %q, want %q, %q", test.path, host, parts, test.host, test.parts)
			}
		})
	}
}

func TestFileURI(t *testing.T) {
	query := url.Values{"_pragma": {"journal_mode(WAL)"}, "_txlock": {"immediate"}}

	host, parts := windowsURIParts(`C:\Users\a b\100%\unison.db`)
	got := fileURI(host, parts, query)
	want := "file:///C:/Users/a%20b/100%25/unison.db?_pragma=journal_mode%28WAL%29&_txlock=immediate"
	if got != want {
		t.Fatalf("fileURI = %q, want %q", got, want)
	}

	host, parts = windowsURIParts(`\\server\share\dir\unison.db`)
	got = fileURI(host, parts, query)
	want = "file://server/share/dir/unison.db?_pragma=journal_mode%28WAL%29&_txlock=immediate"
	if got != want {
		t.Fatalf("fileURI of a UNC path = %q, want %q", got, want)
	}
}

// TestOpenPathWithSpecialCharacters checks that a data directory with a space,
// a percent sign and non-ASCII letters opens a database in the file it names.
func TestOpenPathWithSpecialCharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data 50%", "Кириллица")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	s := openTestStore(t, Options{Dir: dir, Keystore: fake.NewKeystore()})
	if err := s.Settings().SetString(context.Background(), "key", "value"); err != nil {
		t.Fatalf("write a setting: %v", err)
	}
	_ = s.Close()

	if _, err := os.Stat(filepath.Join(dir, DatabaseFile)); err != nil {
		t.Fatalf("the database is not in the data directory: %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) == 0 {
		t.Fatalf("the data directory is empty: %v", err)
	}
}
