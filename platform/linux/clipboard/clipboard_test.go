package clipboard

import (
	"bytes"
	"errors"
	"testing"

	"github.com/lebe-dev/unison/core/ports"
)

func TestSensitiveByTargets(t *testing.T) {
	cases := []struct {
		name    string
		targets []string
		want    bool
	}{
		{name: "hint", targets: []string{atomUTF8String, PasswordManagerHint}, want: true},
		{name: "hint mime", targets: []string{PasswordManagerHintMIME}, want: true},
		{name: "text and html", targets: []string{atomUTF8String, atomTextHTML}},
		{name: "no targets", targets: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sensitive(tc.targets); got != tc.want {
				t.Fatalf("Sensitive(%v) = %v, want %v", tc.targets, got, tc.want)
			}
		})
	}
}

func TestSensitiveHint(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{name: "secret", payload: []byte("secret"), want: true},
		{name: "secret with spaces", payload: []byte(" secret\n"), want: true},
		{name: "alwayssecret", payload: []byte("alwayssecret"), want: true},
		{name: "upper case", payload: []byte("SECRET"), want: true},
		{name: "nul terminated", payload: []byte("secret\x00"), want: true},
		{name: "other value", payload: []byte("clipboard")},
		{name: "empty", payload: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SensitiveHint(tc.payload); got != tc.want {
				t.Fatalf("SensitiveHint(%q) = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}

func TestPayloadOf(t *testing.T) {
	content := ports.Content{
		ItemID: "item-1",
		Representations: []ports.Representation{
			{MIME: MIMETextPlain, Data: []byte("text")},
			{MIME: MIMETextHTML, Data: []byte("<b>text</b>")},
		},
	}
	got, err := payloadOf(content)
	if err != nil {
		t.Fatalf("payloadOf: %v", err)
	}
	if string(got.plain) != "text" || string(got.html) != "<b>text</b>" || got.itemID != "item-1" {
		t.Fatalf("payloadOf() = %+v", got)
	}
}

func TestPayloadOfImage(t *testing.T) {
	png := encodeTestImage(t, MIMEImagePNG)
	got, err := payloadOf(ports.Content{
		ItemID:          "item-2",
		Representations: []ports.Representation{{MIME: MIMEImagePNG, Data: png}},
	})
	if err != nil {
		t.Fatalf("payloadOf: %v", err)
	}
	if !bytes.Equal(got.png, png) || got.itemID != "item-2" {
		t.Fatalf("payloadOf() carries %d png bytes and item id %q", len(got.png), got.itemID)
	}
}

func TestPayloadOfFilesWin(t *testing.T) {
	dir := t.TempDir()
	first := touch(t, dir, "a.txt")
	second := touch(t, dir, "b.txt")
	got, err := payloadOf(ports.Content{
		Representations: []ports.Representation{
			{MIME: MIMETextPlain, Data: []byte("text")},
			{MIME: MIMEImagePNG, Data: encodeTestImage(t, MIMEImagePNG)},
			{MIME: MIMEUnisonFile, FilePath: first},
			{MIME: MIMEUnisonFile, FilePath: second},
		},
	})
	if err != nil {
		t.Fatalf("payloadOf: %v", err)
	}
	if len(got.files) != 2 || got.files[0] != first || got.files[1] != second {
		t.Fatalf("payloadOf() files = %q, want %q", got.files, []string{first, second})
	}
	if got.plain != nil || got.html != nil || got.png != nil {
		t.Fatal("payloadOf() kept text or an image next to files")
	}
}

func TestPayloadOfRejectsUnsupported(t *testing.T) {
	cases := []struct {
		name    string
		content ports.Content
	}{
		{name: "empty", content: ports.Content{}},
		{name: "unknown mime", content: ports.Content{Representations: []ports.Representation{{MIME: "application/pdf", Data: []byte("pdf")}}}},
		{name: "broken png", content: ports.Content{Representations: []ports.Representation{{MIME: MIMEImagePNG, Data: []byte("png")}}}},
		{name: "relative file", content: ports.Content{Representations: []ports.Representation{{MIME: MIMEUnisonFile, FilePath: "a.txt"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := payloadOf(tc.content)
			if !errors.Is(err, ErrUnsupportedContent) {
				t.Fatalf("payloadOf error = %v, want ErrUnsupportedContent", err)
			}
		})
	}
}
