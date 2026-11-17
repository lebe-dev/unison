// Package download prepares the files that arrive from other devices: a name
// that is safe on every platform, a new file that never replaces an existing
// one, and the mark the platform puts on downloaded files, so the system warns
// before it runs one.
package download

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxNameLength bounds a file name in bytes.
	MaxNameLength = 200
	// maxAttempts bounds the lookups of a free file name.
	maxAttempts = 100
	// fallbackName replaces a name that has nothing left after sanitizing.
	fallbackName = "file"
)

// reservedNames are the device names Windows refuses as file names.
var reservedNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// SafeName returns a file name that is safe to write on every platform: no
// directories, no control characters, no invisible formatting characters that
// could disguise the extension, no characters Windows refuses, no reserved
// names and no trailing dots or spaces.
func SafeName(name string) string {
	cleaned := filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	cleaned = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r), strings.ContainsRune(`/<>:"|?*`, r):
			return '-'
		default:
			return r
		}
	}, cleaned)
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" || cleaned == "-" || cleaned == "." || cleaned == ".." {
		return fallbackName
	}
	if len(cleaned) > MaxNameLength {
		cleaned = truncate(cleaned, MaxNameLength)
	}
	if isReservedName(cleaned) {
		cleaned = "_" + cleaned
	}
	return cleaned
}

// Create creates a new file for a received name in dir and marks it as
// downloaded. A taken name gets a counter, so an existing file is never
// replaced. The caller closes the file.
func Create(dir, name string) (string, *os.File, error) {
	name = SafeName(name)
	for attempt := range maxAttempts {
		path := filepath.Join(dir, numberedName(name, attempt))
		//nolint:gosec // The name is sanitized and the path stays in dir.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("download: create file: %w", err)
		}
		if err := Mark(path); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return "", nil, err
		}
		return path, file, nil
	}
	return "", nil, fmt.Errorf("download: no free file name after %d attempts", maxAttempts)
}

// numberedName adds a counter to a file name that is already taken.
func numberedName(name string, attempt int) string {
	if attempt == 0 {
		return name
	}
	extension := filepath.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, extension), attempt, extension)
}

// truncate cuts s to at most limit bytes without splitting a character.
func truncate(s string, limit int) string {
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// isReservedName reports a name Windows refuses, with or without an extension.
func isReservedName(name string) bool {
	base := name
	if index := strings.Index(base, "."); index >= 0 {
		base = base[:index]
	}
	_, reserved := reservedNames[strings.ToLower(strings.TrimSpace(base))]
	return reserved
}
