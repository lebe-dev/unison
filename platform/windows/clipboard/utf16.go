package clipboard

import (
	"encoding/binary"
	"unicode/utf16"
)

// encodeText converts UTF-8 text into the NUL terminated UTF-16LE payload of
// CF_UNICODETEXT.
func encodeText(text []byte) []byte {
	units := utf16.Encode([]rune(string(text)))
	payload := make([]byte, 2*(len(units)+1))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(payload[2*i:], unit)
	}
	return payload
}

// decodeText converts a UTF-16LE payload of CF_UNICODETEXT into UTF-8 without
// the terminator. Invalid sequences become the replacement character.
func decodeText(payload []byte) []byte {
	units := make([]uint16, 0, len(payload)/2)
	for i := 0; i+1 < len(payload); i += 2 {
		unit := binary.LittleEndian.Uint16(payload[i:])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return []byte(string(utf16.Decode(units)))
}
