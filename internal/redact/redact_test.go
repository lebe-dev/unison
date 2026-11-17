package redact

import "testing"

// TestPaths checks that absolute paths are removed from a text.
func TestPaths(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"unix", "open /tmp/unison/cache.db: permission denied", "open [path]: permission denied"},
		{"macOS home", "open /Users/alice/Library/Application Support/Unison/unison.db: permission denied", "open [path]: permission denied"},
		{"path with spaces", "wrote /Users/alice/My Documents/report.pdf", "wrote [path]"},
		{"path followed by words", "cannot read /Users/alice/file because it is locked", "cannot read [path] because it is locked"},
		{"windows path with spaces", `open C:\Program Files\Unison\unison.db: access denied`, "open [path]: access denied"},
		{"macOS private", "read /private/var/folders/ab/cd/T/unison.log: no such file", "read [path]: no such file"},
		{"android data", "mkdir /data/user/0/app.unison/files: read-only file system", "mkdir [path]: read-only file system"},
		{"android storage", "read /storage/emulated/0/Download/file.txt: denied", "read [path]: denied"},
		{"linux home", "stat /home/alice/.config/unison: no such file", "stat [path]: no such file"},
		{"windows", `open C:\Users\alice\AppData\Local\Unison\unison.db: access denied`, "open [path]: access denied"},
		{"windows with slashes", "open C:/Users/alice/unison.db: access denied", "open [path]: access denied"},
		{"quoted", `stat "/root/.config/unison.db": no such file`, `stat "[path]": no such file`},
		{"no path", "the node is not running", "the node is not running"},
		{"url", "see https://example.com/Users/alice/notes", "see https://example.com/Users/alice/notes"},
		{"ordinary words", "the home directory is missing", "the home directory is missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Paths(tc.in); got != tc.want {
				t.Errorf("Paths(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
