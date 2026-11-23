package clipboard

import (
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"
)

// trimNUL drops the trailing NUL some X11 clients append to text values.
func trimNUL(data []byte) []byte {
	for len(data) > 0 && data[len(data)-1] == 0 {
		data = data[:len(data)-1]
	}
	return data
}

// decodePlain converts the payload of a plain text target to UTF-8. STRING and
// TEXT carry latin-1, the other targets carry UTF-8.
func decodePlain(data []byte, latin1 bool) []byte {
	data = trimNUL(data)
	if !latin1 {
		return data
	}
	return latin1ToUTF8(data)
}

// latin1ToUTF8 converts latin-1 bytes to UTF-8.
func latin1ToUTF8(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for _, b := range data {
		out = utf8.AppendRune(out, rune(b))
	}
	return out
}

// encodeLatin1 converts UTF-8 text to latin-1 for the STRING target. Runes
// outside latin-1 become question marks.
func encodeLatin1(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		data = data[size:]
		if r > 0xFF {
			out = append(out, '?')
			continue
		}
		out = append(out, byte(r))
	}
	return out
}

// decodeHTML converts an HTML value to UTF-8. Firefox and Chromium may put HTML
// on the clipboard as UTF-16 with a byte order mark; BOM-less UTF-16 with zero
// high bytes is recognised as well. Other values pass through unchanged.
func decodeHTML(data []byte) []byte {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return utf16ToUTF8(data[2:], binary.LittleEndian)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return utf16ToUTF8(data[2:], binary.BigEndian)
	}
	if order, ok := utf16Order(data); ok {
		return utf16ToUTF8(data, order)
	}
	return trimNUL(data)
}

// utf16Order guesses the byte order of BOM-less UTF-16 by counting zero bytes of
// the high half of the code units. ASCII HTML in UTF-16 has a zero high byte per
// unit.
func utf16Order(data []byte) (binary.ByteOrder, bool) {
	if len(data) < 2 || len(data)%2 != 0 {
		return nil, false
	}
	var low, high int
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] == 0 {
			low++
		}
		if data[i+1] == 0 {
			high++
		}
	}
	units := len(data) / 2
	switch {
	case high > low && high*4 >= units:
		return binary.LittleEndian, true
	case low > high && low*4 >= units:
		return binary.BigEndian, true
	default:
		return nil, false
	}
}

// utf16ToUTF8 decodes UTF-16 code units to UTF-8.
func utf16ToUTF8(data []byte, order binary.ByteOrder) []byte {
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		units = append(units, order.Uint16(data[i:]))
	}
	return []byte(string(utf16.Decode(units)))
}
