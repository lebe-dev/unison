package clipboard

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

// wideList builds the file list part of a CF_HDROP payload.
func wideList(paths ...string) []byte {
	var list []byte
	for _, path := range paths {
		for _, unit := range utf16.Encode([]rune(path)) {
			list = binary.LittleEndian.AppendUint16(list, unit)
		}
		list = binary.LittleEndian.AppendUint16(list, 0)
	}
	return binary.LittleEndian.AppendUint16(list, 0)
}

// dropPayload assembles a CF_HDROP payload from a header and a file list.
func dropPayload(offset uint32, wide bool, list []byte) []byte {
	payload := make([]byte, dropFilesSize)
	binary.LittleEndian.PutUint32(payload[0:], offset)
	if wide {
		binary.LittleEndian.PutUint32(payload[16:], 1)
	}
	return append(payload, list...)
}

func TestDropFilesRoundTrip(t *testing.T) {
	paths := []string{`C:\Users\юзер\отчёт.pdf`, `C:\Temp\a.log`}
	payload, err := encodeDropFiles(paths)
	if err != nil {
		t.Fatalf("encodeDropFiles: %v", err)
	}
	got, err := decodeDropFiles(payload)
	if err != nil {
		t.Fatalf("decodeDropFiles: %v", err)
	}
	if len(got) != len(paths) {
		t.Fatalf("paths = %v, want %v", got, paths)
	}
	for i, path := range paths {
		if got[i] != path {
			t.Fatalf("path %d = %q, want %q", i, got[i], path)
		}
	}
}

func TestEncodeDropFilesLayout(t *testing.T) {
	payload, err := encodeDropFiles([]string{`C:\a.txt`})
	if err != nil {
		t.Fatalf("encodeDropFiles: %v", err)
	}
	if got := binary.LittleEndian.Uint32(payload[0:]); got != dropFilesSize {
		t.Fatalf("file list offset = %d, want %d", got, dropFilesSize)
	}
	if got := binary.LittleEndian.Uint32(payload[16:]); got != 1 {
		t.Fatalf("wide flag = %d, want 1", got)
	}
	if want := dropFilesSize + 2*(len(`C:\a.txt`)+2); len(payload) != want {
		t.Fatalf("payload is %d bytes, want %d", len(payload), want)
	}
	if got := payload[len(payload)-4:]; !bytes.Equal(got, make([]byte, 4)) {
		t.Fatalf("payload ends with %v, want two NUL terminators", got)
	}
}

func TestEncodeDropFilesRejectsInvalidPaths(t *testing.T) {
	tests := map[string][]string{
		"empty list":    nil,
		"empty path":    {""},
		"path with nul": {`C:\a.txt`, "C:\\b\x00.txt"},
		"invalid utf-8": {"C:\\\xff.txt"},
		"too long":      {string(bytes.Repeat([]byte("a"), maxDropPath+1))},
		"too many":      make([]string, maxDropFiles+1),
	}
	for name, paths := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := encodeDropFiles(paths); err == nil {
				t.Fatal("encodeDropFiles accepted the paths")
			}
		})
	}
}

func TestDecodeDropFilesRejectsBrokenPayloads(t *testing.T) {
	tests := map[string][]byte{
		"empty":             nil,
		"short header":      make([]byte, dropFilesSize-1),
		"offset inside":     dropPayload(dropFilesSize-1, true, wideList(`C:\a.txt`)),
		"offset beyond":     dropPayload(uint32(dropFilesSize+len(`C:\a.txt`)+16), true, wideList(`C:\a.txt`)),
		"missing end":       dropPayload(dropFilesSize, true, wideList(`C:\a.txt`)[:2*len(`C:\a.txt`)+2]),
		"no terminator":     dropPayload(dropFilesSize, true, []byte(`C:\a.txt`)),
		"odd byte list":     dropPayload(dropFilesSize, true, []byte{0x43}),
		"invalid byte list": dropPayload(dropFilesSize, false, []byte("C:\\\xff.txt\x00\x00")),
		"byte list endless": dropPayload(dropFilesSize, false, []byte("C:\\a.txt")),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if paths, err := decodeDropFiles(payload); err == nil {
				t.Fatalf("decodeDropFiles returned %v, want an error", paths)
			}
		})
	}
}

func TestDecodeDropFilesByteList(t *testing.T) {
	payload := dropPayload(dropFilesSize, false, append([]byte("C:\\a.txt\x00C:\\b.txt"), 0, 0))
	got, err := decodeDropFiles(payload)
	if err != nil {
		t.Fatalf("decodeDropFiles: %v", err)
	}
	want := []string{`C:\a.txt`, `C:\b.txt`}
	if len(got) != len(want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDecodeDropFilesEmptyList(t *testing.T) {
	payload := dropPayload(dropFilesSize, true, wideList())
	got, err := decodeDropFiles(payload)
	if err != nil {
		t.Fatalf("decodeDropFiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("paths = %v, want none", got)
	}
}

func TestDecodeDropFilesCapsCount(t *testing.T) {
	paths := make([]string, maxDropFiles+50)
	for i := range paths {
		paths[i] = `C:\file.txt`
	}
	payload, err := encodeDropFiles(paths[:maxDropFiles])
	if err != nil {
		t.Fatalf("encodeDropFiles: %v", err)
	}
	payload = append(payload[:len(payload)-2], wideList(paths...)...)
	got, err := decodeDropFiles(payload)
	if err != nil {
		t.Fatalf("decodeDropFiles: %v", err)
	}
	if len(got) != maxDropFiles {
		t.Fatalf("paths = %d, want the cap %d", len(got), maxDropFiles)
	}
}
