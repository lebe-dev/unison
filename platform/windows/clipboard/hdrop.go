package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The clipboard file format is a DROPFILES header followed by a list of NUL
// terminated paths and an extra NUL that ends the list.
const (
	// dropFilesSize is the size of DROPFILES.
	dropFilesSize = 20
	// dropFilesWide marks the paths as UTF-16LE.
	dropFilesWide = 1
	// maxDropFiles bounds the file list of one payload. The list comes from
	// another application, so its length is never trusted.
	maxDropFiles = 256
	// maxDropPath bounds one path in bytes of UTF-8.
	maxDropPath = 4096
)

// errDropList reports a file list that cannot be read or written.
var errDropList = errors.New("clipboard: malformed file list")

// encodeDropFiles builds the CF_HDROP payload of file paths.
func encodeDropFiles(paths []string) ([]byte, error) {
	switch {
	case len(paths) == 0:
		return nil, fmt.Errorf("%w: no files", errDropList)
	case len(paths) > maxDropFiles:
		return nil, fmt.Errorf("%w: %d files exceed the limit %d", errDropList, len(paths), maxDropFiles)
	}

	payload := make([]byte, 0, dropFilesSize+64*len(paths))
	payload = binary.LittleEndian.AppendUint32(payload, dropFilesSize)
	payload = binary.LittleEndian.AppendUint32(payload, 0)
	payload = binary.LittleEndian.AppendUint32(payload, 0)
	payload = binary.LittleEndian.AppendUint32(payload, 0)
	payload = binary.LittleEndian.AppendUint32(payload, dropFilesWide)
	for _, path := range paths {
		if path == "" || len(path) > maxDropPath || strings.ContainsRune(path, 0) || !utf8.ValidString(path) {
			return nil, fmt.Errorf("%w: invalid path of %d bytes", errDropList, len(path))
		}
		for _, unit := range utf16.Encode([]rune(path)) {
			payload = binary.LittleEndian.AppendUint16(payload, unit)
		}
		payload = binary.LittleEndian.AppendUint16(payload, 0)
	}
	payload = binary.LittleEndian.AppendUint16(payload, 0)
	return payload, nil
}

// decodeDropFiles returns the paths of a CF_HDROP payload. A list of plain byte
// paths is read as UTF-8.
func decodeDropFiles(payload []byte) ([]string, error) {
	if len(payload) < dropFilesSize {
		return nil, fmt.Errorf("%w: %d bytes have no header", errDropList, len(payload))
	}
	offset := int(binary.LittleEndian.Uint32(payload))
	if offset < dropFilesSize || offset >= len(payload) {
		return nil, fmt.Errorf("%w: file list starts at %d", errDropList, offset)
	}
	if binary.LittleEndian.Uint32(payload[16:]) != 0 {
		return decodeWidePaths(payload[offset:])
	}
	return decodeBytePaths(payload[offset:])
}

// decodeWidePaths returns the UTF-16LE paths of a file list.
func decodeWidePaths(list []byte) ([]string, error) {
	paths := make([]string, 0, 4)
	for pos := 0; pos < len(list) && len(paths) < maxDropFiles; {
		end := pos
		for end+1 < len(list) && binary.LittleEndian.Uint16(list[end:]) != 0 {
			end += 2
		}
		if end+1 >= len(list) {
			return nil, fmt.Errorf("%w: path at byte %d has no terminator", errDropList, pos)
		}
		if end == pos {
			return paths, nil
		}
		units := make([]uint16, 0, (end-pos)/2)
		for i := pos; i < end; i += 2 {
			units = append(units, binary.LittleEndian.Uint16(list[i:]))
		}
		paths = append(paths, string(utf16.Decode(units)))
		pos = end + 2
	}
	if len(paths) == maxDropFiles {
		return paths, nil
	}
	return nil, fmt.Errorf("%w: file list has no end", errDropList)
}

// decodeBytePaths returns the paths of a list without the wide flag.
func decodeBytePaths(list []byte) ([]string, error) {
	paths := make([]string, 0, 4)
	for pos := 0; pos < len(list) && len(paths) < maxDropFiles; {
		end := bytes.IndexByte(list[pos:], 0)
		if end < 0 {
			return nil, fmt.Errorf("%w: path at byte %d has no terminator", errDropList, pos)
		}
		if end == 0 {
			return paths, nil
		}
		path := list[pos : pos+end]
		if !utf8.Valid(path) {
			return nil, fmt.Errorf("%w: path at byte %d is not utf-8", errDropList, pos)
		}
		paths = append(paths, string(path))
		pos += end + 1
	}
	if len(paths) == maxDropFiles {
		return paths, nil
	}
	return nil, fmt.Errorf("%w: file list has no end", errDropList)
}
