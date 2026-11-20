package download

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestParseUserDirs checks the download folder of a user-dirs.dirs file.
func TestParseUserDirs(t *testing.T) {
	const home = "/home/user"

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "localized name",
			data: `XDG_DOWNLOAD_DIR="$HOME/Загрузки"`,
			want: "/home/user/Загрузки",
		},
		{
			name: "plain name",
			data: "XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n",
			want: "/home/user/Downloads",
		},
		{
			name: "unquoted",
			data: "XDG_DOWNLOAD_DIR=$HOME/Загрузки\n",
			want: "/home/user/Загрузки",
		},
		{
			name: "braced home",
			data: `XDG_DOWNLOAD_DIR="${HOME}/Downloads"`,
			want: "/home/user/Downloads",
		},
		{
			name: "single quotes are literal",
			data: "XDG_DOWNLOAD_DIR='$HOME/Downloads'",
			want: "",
		},
		{
			name: "relative path",
			data: `XDG_DOWNLOAD_DIR="Downloads"`,
			want: "",
		},
		{
			name: "home itself disables the folder",
			data: `XDG_DOWNLOAD_DIR="$HOME/"`,
			want: "",
		},
		{
			name: "home without slash disables the folder",
			data: `XDG_DOWNLOAD_DIR="$HOME"`,
			want: "",
		},
		{
			name: "comments and blank lines",
			data: "# written by the desktop session\n\nXDG_DESKTOP_DIR=\"$HOME/Рабочий стол\"\nXDG_DOWNLOAD_DIR=\"$HOME/Загрузки\"\n",
			want: "/home/user/Загрузки",
		},
		{
			name: "comment after the value",
			data: "XDG_DOWNLOAD_DIR=\"$HOME/Downloads\" # moved\n",
			want: "/home/user/Downloads",
		},
		{
			name: "hash inside the path",
			data: "XDG_DOWNLOAD_DIR=\"$HOME/In # box\"\n",
			want: "/home/user/In # box",
		},
		{
			name: "last assignment wins",
			data: "XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\nXDG_DOWNLOAD_DIR=\"$HOME/Загрузки\"\n",
			want: "/home/user/Загрузки",
		},
		{
			name: "windows line endings",
			data: "XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\r\n",
			want: "/home/user/Downloads",
		},
		{
			name: "trailing slash",
			data: `XDG_DOWNLOAD_DIR="$HOME/Downloads/"`,
			want: "/home/user/Downloads",
		},
		{
			name: "escaped quote",
			data: `XDG_DOWNLOAD_DIR="$HOME/My \"Stuff\""`,
			want: `/home/user/My "Stuff"`,
		},
		{
			name: "spaces in the name",
			data: `XDG_DOWNLOAD_DIR="$HOME/My Downloads"`,
			want: "/home/user/My Downloads",
		},
		{
			name: "missing key",
			data: `XDG_DESKTOP_DIR="$HOME/Desktop"`,
			want: "",
		},
		{
			name: "empty file",
			data: "",
			want: "",
		},
		{
			name: "abbreviated key",
			data: `XDG_DOWNLOAD_DIRS="$HOME/Downloads"`,
			want: "",
		},
		{
			name: "space around the equals sign",
			data: ` XDG_DOWNLOAD_DIR = "$HOME/Downloads" `,
			want: "/home/user/Downloads",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseUserDirs(test.data, home); got != test.want {
				t.Fatalf("parseUserDirs() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestDefaultDir checks that received files land in a Unison folder of the
// download directory.
func TestDefaultDir(t *testing.T) {
	dir := DefaultDir()
	if dir == "" {
		t.Skip("the home directory is not known")
	}
	if filepath.Base(dir) != downloadsDirName {
		t.Fatalf("default directory is %q, want the last part %q", dir, downloadsDirName)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("default directory %q is relative", dir)
	}
	if strings.Contains(dir, "..") {
		t.Fatalf("default directory %q is not clean", dir)
	}
}
