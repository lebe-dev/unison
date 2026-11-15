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

func TestTextOf(t *testing.T) {
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
			got, err := textOf(test.content)
			if err != nil {
				t.Fatalf("textOf: %v", err)
			}
			if got.hasPlain != test.want.hasPlain || got.hasHTML != test.want.hasHTML {
				t.Fatalf("textOf = %+v, want %+v", got, test.want)
			}
			if !bytes.Equal(got.plain, test.want.plain) || !bytes.Equal(got.html, test.want.html) {
				t.Fatalf("textOf payloads = %q, %q; want %q, %q", got.plain, got.html, test.want.plain, test.want.html)
			}
		})
	}
}

func TestTextOfRejectsUnsupportedContent(t *testing.T) {
	tests := []ports.Content{
		{},
		{Representations: []ports.Representation{{MIME: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}}}},
	}
	for _, content := range tests {
		if _, err := textOf(content); !errors.Is(err, ErrUnsupportedContent) {
			t.Fatalf("textOf error = %v, want ErrUnsupportedContent", err)
		}
	}
}
