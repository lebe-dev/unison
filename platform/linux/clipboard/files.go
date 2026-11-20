package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// MIME types of a copied file list and of a file representation.
const (
	// MIMEGNOMECopiedFiles is the Nautilus target of a copied file list. Its
	// first line is "copy" or "cut", the remaining lines are file URIs.
	MIMEGNOMECopiedFiles = "x-special/gnome-copied-files"
	// MIMEURIList is the RFC 2483 list of copied URIs.
	MIMEURIList = "text/uri-list"
	// MIMEUnisonFile marks a representation that carries a file path.
	MIMEUnisonFile = "application/x-unison-file"
)

const (
	// maxFiles bounds the file list of one clipboard value.
	maxFiles = 256
	// maxPathURI bounds one file URI in bytes.
	maxPathURI = 4096
	// maxParsedLines bounds the lines read from one file list. The value comes
	// from another application, so its length is never trusted.
	maxParsedLines = 4 * maxFiles
)

// errFileList reports a file list that cannot be written.
var errFileList = errors.New("clipboard: invalid file list")

// pickFiles returns the target that carries a copied file list.
func (t targetList) pickFiles() string {
	for _, candidate := range []string{MIMEGNOMECopiedFiles, MIMEURIList} {
		if t.has(candidate) {
			return candidate
		}
	}
	return ""
}

// parseGNOMECopiedFiles returns the URIs of a Nautilus file list. The header
// line is optional; a cut is read the same way as a copy.
func parseGNOMECopiedFiles(payload []byte) []string {
	lines := splitLines(payload)
	if len(lines) > 0 && (lines[0] == "copy" || lines[0] == "cut") {
		lines = lines[1:]
	}
	return collectURIs(lines)
}

// parseURIList returns the URIs of an RFC 2483 list. Empty lines and comments
// are skipped.
func parseURIList(payload []byte) []string {
	lines := make([]string, 0, 8)
	for _, line := range splitLines(payload) {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return collectURIs(lines)
}

// splitLines splits the first maxParsedLines lines of a text value into trimmed
// lines. Both CRLF and LF endings are accepted.
func splitLines(payload []byte) []string {
	lines := make([]string, 0, 8)
	for len(payload) > 0 && len(lines) < maxParsedLines {
		line, rest, found := bytes.Cut(payload, []byte{'\n'})
		if !found {
			line, rest = payload, nil
		}
		lines = append(lines, strings.TrimSpace(strings.TrimSuffix(string(line), "\r")))
		payload = rest
	}
	return lines
}

// collectURIs keeps the first maxFiles URIs of a list.
func collectURIs(lines []string) []string {
	uris := make([]string, 0, min(len(lines), maxFiles))
	for _, line := range lines {
		if len(uris) >= maxFiles {
			break
		}
		uris = append(uris, line)
	}
	return uris
}

// filePaths returns the existing regular files of the URIs. A URI of another
// host, a malformed one, a directory and a missing path are left out.
func filePaths(uris []string) []string {
	paths := make([]string, 0, len(uris))
	seen := make(map[string]struct{}, len(uris))
	for _, uri := range uris {
		if len(paths) >= maxFiles {
			break
		}
		path, ok := pathOfURI(uri)
		if !ok {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

// pathOfURI returns the local path of a file URI. Only the local host is
// accepted.
func pathOfURI(uri string) (string, bool) {
	if len(uri) > maxPathURI {
		return "", false
	}
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return "", false
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	path := parsed.Path
	if path == "" || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", false
	}
	return path, true
}

// pathToURI returns the file URI of a local path.
func pathToURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// checkedPaths validates the paths of content to write. Files win over text and
// an image, so the paths are the whole payload.
func checkedPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w: no files", errFileList)
	}
	checked := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if len(path) == 0 || len(path) > maxPathURI || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%w: invalid path of %d bytes", errFileList, len(path))
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		checked = append(checked, path)
	}
	if len(checked) > maxFiles {
		return nil, fmt.Errorf("%w: %d files exceed the limit %d", errFileList, len(checked), maxFiles)
	}
	return checked, nil
}

// encodeURIList builds the RFC 2483 payload of file paths.
func encodeURIList(paths []string) []byte {
	var payload strings.Builder
	for _, path := range paths {
		payload.WriteString(pathToURI(path))
		payload.WriteString("\r\n")
	}
	return []byte(payload.String())
}

// encodeGNOMECopiedFiles builds the Nautilus payload of copied file paths.
func encodeGNOMECopiedFiles(paths []string) []byte {
	var payload strings.Builder
	payload.WriteString("copy\n")
	for _, path := range paths {
		payload.WriteString(pathToURI(path))
		payload.WriteString("\n")
	}
	return []byte(payload.String())
}

// encodePlainPaths builds the newline separated list of file paths for plain
// text targets.
func encodePlainPaths(paths []string) []byte {
	var payload strings.Builder
	for _, path := range paths {
		payload.WriteString(path)
		payload.WriteString("\n")
	}
	return []byte(payload.String())
}
