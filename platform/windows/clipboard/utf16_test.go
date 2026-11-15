package clipboard

import (
	"bytes"
	"testing"
)

func TestEncodeText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []byte
	}{
		{name: "ascii", text: "hi", want: []byte{'h', 0, 'i', 0, 0, 0}},
		{name: "empty", text: "", want: []byte{0, 0}},
		{
			name: "cyrillic",
			// Each Cyrillic letter is one UTF-16 code unit.
			text: "ю",
			want: []byte{0x4e, 0x04, 0, 0},
		},
		{
			name: "emoji",
			// One rune outside the basic plane is a surrogate pair.
			text: "🙂",
			want: []byte{0x3d, 0xd8, 0x42, 0xde, 0, 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := encodeText([]byte(test.text)); !bytes.Equal(got, test.want) {
				t.Fatalf("encodeText(%q) = % x, want % x", test.text, got, test.want)
			}
		})
	}
}

func TestDecodeText(t *testing.T) {
	payload := []byte{'h', 0, 'i', 0, 0, 0, 'x', 0}
	if got := string(decodeText(payload)); got != "hi" {
		t.Fatalf("decodeText = %q, want %q", got, "hi")
	}
}

func TestDecodeTextWithoutTerminator(t *testing.T) {
	payload := encodeText([]byte("no terminator"))
	payload = payload[:len(payload)-2]
	if got := string(decodeText(payload)); got != "no terminator" {
		t.Fatalf("decodeText = %q, want %q", got, "no terminator")
	}
}

func TestDecodeTextOddLength(t *testing.T) {
	if got := string(decodeText([]byte{'a', 0, 'b'})); got != "a" {
		t.Fatalf("decodeText = %q, want %q", got, "a")
	}
}

func TestDecodeTextInvalidSurrogate(t *testing.T) {
	// A lone high surrogate has no valid UTF-8 form.
	if got := string(decodeText([]byte{0x00, 0xd8})); got != "\uFFFD" {
		t.Fatalf("decodeText = %q, want the replacement character", got)
	}
}

func TestTextRoundTrip(t *testing.T) {
	for _, text := range []string{"", "plain", "привет", "🙂 emoji", "line\nbreaks\tand tabs"} {
		if got := string(decodeText(encodeText([]byte(text)))); got != text {
			t.Fatalf("round trip of %q = %q", text, got)
		}
	}
}
