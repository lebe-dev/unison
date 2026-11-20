package download

import (
	"path/filepath"
	"strings"
)

// userDirDownloadKey is the key of the download folder in a user-dirs.dirs
// file.
const userDirDownloadKey = "XDG_DOWNLOAD_DIR"

// downloadsDirName is the folder of received files inside the download
// directory of the user.
const downloadsDirName = "Unison"

// DefaultDir returns the directory that receives incoming files: the download
// folder of the desktop session with Unison below it. The directory is empty
// when the home directory is not known.
func DefaultDir() string {
	base := downloadBase()
	if base == "" {
		return ""
	}
	return filepath.Join(base, downloadsDirName)
}

// parseUserDirs returns the download folder of a user-dirs.dirs file, or an
// empty string when the file does not name one. The format is a shell
// assignment, so comments, quoting and $HOME have to be handled.
func parseUserDirs(data, home string) string {
	value := ""
	found := false
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != userDirDownloadKey {
			continue
		}
		value, found = strings.TrimSpace(rest), true
	}
	if !found {
		return ""
	}

	dir := userDirValue(value, home)
	// A desktop session that turns the folder off writes the home directory.
	if dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) == filepath.Clean(home) {
		return ""
	}
	return filepath.Clean(dir)
}

// userDirValue returns the path of an assignment value, with $HOME expanded in
// the forms a desktop session writes.
func userDirValue(value, home string) string {
	switch {
	case strings.HasPrefix(value, `"`):
		value = unquoteDouble(value[1:])
	case strings.HasPrefix(value, "'"):
		end := strings.IndexByte(value[1:], '\'')
		if end < 0 {
			return ""
		}
		// Single quotes keep the text as it is.
		return value[1 : 1+end]
	default:
		if index := strings.IndexByte(value, '#'); index >= 0 {
			value = value[:index]
		}
		value = strings.TrimSpace(value)
	}
	return expandHome(value, home)
}

// unquoteDouble returns the text of a double quoted value, which ends at the
// closing quote.
func unquoteDouble(text string) string {
	var b strings.Builder
	escaped := false
	for _, r := range text {
		switch {
		case escaped:
			// Only the characters a double quoted shell word escapes lose the
			// backslash.
			if r != '"' && r != '\\' && r != '$' && r != '`' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			return b.String()
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// expandHome replaces the forms of the home directory in a value.
func expandHome(value, home string) string {
	value = strings.ReplaceAll(value, "${HOME}", home)
	return strings.ReplaceAll(value, "$HOME", home)
}
