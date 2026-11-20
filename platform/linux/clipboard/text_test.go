package clipboard

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

// utf16Bytes encodes text as UTF-16 with the given byte order and an optional
// byte order mark.
func utf16Bytes(text string, order binary.ByteOrder, bom bool) []byte {
	var data []byte
	if bom {
		if order == binary.LittleEndian {
			data = append(data, 0xFF, 0xFE)
		} else {
			data = append(data, 0xFE, 0xFF)
		}
	}
	for _, unit := range utf16.Encode([]rune(text)) {
		var pair [2]byte
		order.PutUint16(pair[:], unit)
		data = append(data, pair[:]...)
	}
	return data
}

func TestDecodeHTMLUTF16(t *testing.T) {
	want := []byte("<p>Привет, мир</p>")
	cases := []struct {
		name  string
		order binary.ByteOrder
		bom   bool
	}{
		{name: "little endian with bom", order: binary.LittleEndian, bom: true},
		{name: "big endian with bom", order: binary.BigEndian, bom: true},
		{name: "little endian without bom", order: binary.LittleEndian},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeHTML(utf16Bytes(string(want), tc.order, tc.bom))
			if !bytes.Equal(got, want) {
				t.Fatalf("decodeHTML() = %q, want %q", got, want)
			}
		})
	}
}

func TestDecodeHTMLSurrogatePair(t *testing.T) {
	text := "<p>emoji: \U0001F600</p>"
	got := decodeHTML(utf16Bytes(text, binary.LittleEndian, true))
	if string(got) != text {
		t.Fatalf("decodeHTML() = %q, want %q", got, text)
	}
}

func TestDecodeHTMLUTF8PassesThrough(t *testing.T) {
	data := []byte("<b>hello</b>")
	if got := decodeHTML(data); !bytes.Equal(got, data) {
		t.Fatalf("decodeHTML() = %q, want %q", got, data)
	}
}

func TestDecodeHTMLTrimsTrailingNUL(t *testing.T) {
	data := append([]byte("<i>x</i>"), 0)
	if got := decodeHTML(data); string(got) != "<i>x</i>" {
		t.Fatalf("decodeHTML() = %q, want %q", got, "<i>x</i>")
	}
}

func TestDecodePlainUTF8(t *testing.T) {
	data := append([]byte("hello"), 0)
	if got := decodePlain(data, false); string(got) != "hello" {
		t.Fatalf("decodePlain() = %q, want %q", got, "hello")
	}
}

func TestDecodePlainLatin1(t *testing.T) {
	// 0xE9 is e with an acute accent in latin-1.
	got := decodePlain([]byte{'c', 'a', 'f', 0xE9}, true)
	if string(got) != "café" {
		t.Fatalf("decodePlain() = %q, want %q", got, "café")
	}
}

func TestEncodeLatin1(t *testing.T) {
	got := encodeLatin1([]byte("café €"))
	if want := []byte{'c', 'a', 'f', 0xE9, ' ', '?'}; !bytes.Equal(got, want) {
		t.Fatalf("encodeLatin1() = %v, want %v", got, want)
	}
}
