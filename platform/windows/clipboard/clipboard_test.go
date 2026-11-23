package clipboard

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

func TestNewDefaults(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := c.openTimeout, DefaultOpenTimeout; got != want {
		t.Fatalf("openTimeout = %v, want %v", got, want)
	}
}

func TestNewCustomTimeout(t *testing.T) {
	c, err := New(Options{OpenTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := c.openTimeout, 50*time.Millisecond; got != want {
		t.Fatalf("openTimeout = %v, want %v", got, want)
	}
}

func TestNewRejectsNegativeTimeout(t *testing.T) {
	if _, err := New(Options{OpenTimeout: -time.Second}); err == nil {
		t.Fatal("New with a negative timeout returned no error")
	}
}

func TestPayloadOfText(t *testing.T) {
	tests := []struct {
		name    string
		content ports.Content
		want    textPayload
	}{
		{
			name: "plain and html",
			content: ports.Content{Representations: []ports.Representation{
				{MIME: MIMETextPlain, Data: []byte("hello")},
				{MIME: MIMETextHTML, Data: []byte("<b>hello</b>")},
			}},
			want: textPayload{plain: []byte("hello"), html: []byte("<b>hello</b>"), hasPlain: true, hasHTML: true},
		},
		{
			name: "html only",
			content: ports.Content{Representations: []ports.Representation{
				{MIME: MIMETextHTML, Data: []byte("<i>only</i>")},
			}},
			want: textPayload{html: []byte("<i>only</i>"), hasHTML: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := payloadOf(test.content)
			if err != nil {
				t.Fatalf("payloadOf: %v", err)
			}
			if got.text.hasPlain != test.want.hasPlain || got.text.hasHTML != test.want.hasHTML {
				t.Fatalf("payloadOf text = %+v, want %+v", got.text, test.want)
			}
			if !bytes.Equal(got.text.plain, test.want.plain) || !bytes.Equal(got.text.html, test.want.html) {
				t.Fatalf("payloadOf payloads = %q, %q; want %q, %q", got.text.plain, got.text.html, test.want.plain, test.want.html)
			}
			if got.png != nil || len(got.files) != 0 {
				t.Fatalf("payloadOf returned image or files for text: %+v", got)
			}
		})
	}
}

func TestPayloadOfImage(t *testing.T) {
	data := []byte("\x89PNG\r\n\x1a\n")
	got, err := payloadOf(ports.Content{Representations: []ports.Representation{
		{MIME: MIMEImagePNG, Data: data},
	}})
	if err != nil {
		t.Fatalf("payloadOf: %v", err)
	}
	if !bytes.Equal(got.png, data) {
		t.Fatalf("payloadOf png = %q, want %q", got.png, data)
	}
}

func TestPayloadOfFiles(t *testing.T) {
	paths := []string{"/tmp/a.txt", "/tmp/b.png"}
	representations := make([]ports.Representation, 0, len(paths))
	for _, path := range paths {
		representations = append(representations, ports.Representation{MIME: "application/x-unison-file", FilePath: path})
	}
	got, err := payloadOf(ports.Content{Representations: representations})
	if err != nil {
		t.Fatalf("payloadOf: %v", err)
	}
	if len(got.files) != len(paths) {
		t.Fatalf("payloadOf files = %v, want %v", got.files, paths)
	}
	for i, path := range paths {
		if got.files[i] != path {
			t.Fatalf("payloadOf file %d = %q, want %q", i, got.files[i], path)
		}
	}
}

func TestPayloadOfRejectsUnsupportedContent(t *testing.T) {
	tests := []ports.Content{
		{},
		{Representations: []ports.Representation{{MIME: "image/gif", Data: []byte("GIF89a")}}},
	}
	for _, content := range tests {
		if _, err := payloadOf(content); !errors.Is(err, ErrUnsupportedContent) {
			t.Fatalf("payloadOf error = %v, want ErrUnsupportedContent", err)
		}
	}
}
