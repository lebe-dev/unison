package share

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// reservedNames are the device names Windows refuses as file names.
var reservedNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// downloadSink writes received files into the downloads directory.
type downloadSink struct {
	// dir returns the directory of the files.
	dir func(ctx context.Context) (string, error)
}

// Write stores one file and returns its path.
func (d downloadSink) Write(ctx context.Context, file File, src io.Reader) (string, error) {
	dir, err := d.dir(ctx)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("share: no downloads directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("share: create downloads directory: %w", err)
	}

	path, target, err := createFile(dir, file.Name)
	if err != nil {
		return "", err
	}
	written, err := io.Copy(target, src)
	closeErr := target.Close()
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("share: write file: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("share: close file: %w", closeErr)
	}
	if written != file.Size {
		_ = os.Remove(path)
		return "", fmt.Errorf("%w: wrote %d bytes of %d", ErrInvalidFile, written, file.Size)
	}
	return path, nil
}

// createFile creates the first free file of the directory.
func createFile(dir, name string) (string, *os.File, error) {
	name = SafeName(name)
	for attempt := range maxDownloadAttempts {
		path := filepath.Join(dir, numberedName(name, attempt))
		//nolint:gosec // The name is sanitized and the path stays in the download directory.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("share: create file: %w", err)
		}
		return path, file, nil
	}
	return "", nil, fmt.Errorf("share: no free file name after %d attempts", maxDownloadAttempts)
}

// SafeName returns a file name that is safe to write on every platform: no
// directories, no control characters, no reserved names and no trailing dots or
// spaces.
func SafeName(name string) string {
	cleaned := filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	cleaned = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '-'
		}
		return r
	}, cleaned)
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "file"
	}
	if len(cleaned) > MaxNameLength {
		cleaned = strings.ToValidUTF8(cleaned[:MaxNameLength], "")
	}
	if isReservedName(cleaned) {
		cleaned = "_" + cleaned
	}
	return cleaned
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

// numberedName adds a counter to a file name that is already taken.
func numberedName(name string, attempt int) string {
	if attempt == 0 {
		return name
	}
	extension := filepath.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, extension), attempt, extension)
}

// checkName reports a file name that cannot be used.
func checkName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: empty name", ErrInvalidFile)
	case !utf8.ValidString(name):
		return fmt.Errorf("%w: name is not valid UTF-8", ErrInvalidFile)
	case len(name) > MaxNameLength:
		return fmt.Errorf("%w: name of %d bytes", ErrInvalidFile, len(name))
	}
	return nil
}
