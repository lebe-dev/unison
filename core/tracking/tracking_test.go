package tracking

import "testing"

func TestStripURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		changed bool
	}{
		{
			name:    "campaign parameters are removed",
			raw:     "https://example.com/page?utm_source=news&utm_medium=mail&id=42",
			want:    "https://example.com/page?id=42",
			changed: true,
		},
		{
			name:    "click identifiers are removed",
			raw:     "https://example.com/?fbclid=abc&gclid=def&msclkid=ghi&yclid=jkl",
			want:    "https://example.com/",
			changed: true,
		},
		{
			name:    "names are compared without case",
			raw:     "https://example.com/?UTM_Source=news&FBCLID=abc&q=go",
			want:    "https://example.com/?q=go",
			changed: true,
		},
		{
			name:    "the order and the encoding of the kept parameters are untouched",
			raw:     "https://example.com/?b=%2F%20x&utm_campaign=ad&a=1+2",
			want:    "https://example.com/?b=%2F%20x&a=1+2",
			changed: true,
		},
		{
			name:    "an encoded name is recognized",
			raw:     "https://example.com/?utm%5Fsource=news&q=go",
			want:    "https://example.com/?q=go",
			changed: true,
		},
		{
			name:    "the fragment is kept",
			raw:     "https://example.com/page?utm_source=news&q=go#part",
			want:    "https://example.com/page?q=go#part",
			changed: true,
		},
		{
			name:    "a fragment without a query is untouched",
			raw:     "https://example.com/page#utm_source",
			want:    "https://example.com/page#utm_source",
			changed: false,
		},
		{
			name:    "a parameter without a value is removed",
			raw:     "https://example.com/?utm_source&q=go",
			want:    "https://example.com/?q=go",
			changed: true,
		},
		{
			name:    "a lookalike parameter is kept",
			raw:     "https://example.com/?ref=home&source=menu&utmish=1",
			want:    "https://example.com/?ref=home&source=menu&utmish=1",
			changed: false,
		},
		{
			name:    "another scheme is untouched",
			raw:     "ftp://example.com/?utm_source=news",
			want:    "ftp://example.com/?utm_source=news",
			changed: false,
		},
		{
			name:    "a URL without a host is untouched",
			raw:     "https:///page?utm_source=news",
			want:    "https:///page?utm_source=news",
			changed: false,
		},
		{
			name:    "a relative URL is untouched",
			raw:     "/page?utm_source=news",
			want:    "/page?utm_source=news",
			changed: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed := StripURL(test.raw)
			if got != test.want || changed != test.changed {
				t.Fatalf("StripURL(%q) = %q, %t; want %q, %t", test.raw, got, changed, test.want, test.changed)
			}
		})
	}
}

func TestStripText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "a URL is cleaned inside a sentence",
			text: "Look at https://example.com/?utm_source=news now.",
			want: "Look at https://example.com/ now.",
		},
		{
			name: "the punctuation of the sentence is kept",
			text: "Read (https://example.com/?fbclid=abc), then https://example.com/?gclid=def!",
			want: "Read (https://example.com/), then https://example.com/!",
		},
		{
			name: "a link with a parenthesis keeps it",
			text: "https://example.com/wiki/Thing_(other)?utm_source=news",
			want: "https://example.com/wiki/Thing_(other)",
		},
		{
			name: "a clean URL is untouched",
			text: "https://example.com/page?a=1",
			want: "https://example.com/page?a=1",
		},
		{
			name: "text without a URL is untouched",
			text: "no links here, utm_source=news",
			want: "no links here, utm_source=news",
		},
		{
			name: "an encoded ampersand does not hide the parameter",
			text: "https://example.com/?a=1&utm_source=news",
			want: "https://example.com/?a=1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StripText(test.text); got != test.want {
				t.Fatalf("StripText(%q) = %q; want %q", test.text, got, test.want)
			}
		})
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "the href attribute and the text are cleaned",
			html: `<a href="https://example.com/?utm_source=news&q=go">https://example.com/?utm_source=news&q=go</a>`,
			want: `<a href="https://example.com/?q=go">https://example.com/?q=go</a>`,
		},
		{
			name: "escaped ampersands are preserved",
			html: `<a href="https://example.com/?utm_source=news&amp;q=go">example</a>`,
			want: `<a href="https://example.com/?q=go">example</a>`,
		},
		{
			name: "the src attribute is cleaned",
			html: `<img src='https://example.com/pixel?fbclid=abc&q=1'>`,
			want: `<img src='https://example.com/pixel?q=1'>`,
		},
		{
			name: "a clean fragment is untouched",
			html: `<a href="https://example.com/?q=go">example</a>`,
			want: `<a href="https://example.com/?q=go">example</a>`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StripHTML(test.html); got != test.want {
				t.Fatalf("StripHTML(%q) = %q; want %q", test.html, got, test.want)
			}
		})
	}
}

func TestStripTextIsIdempotent(t *testing.T) {
	inputs := []string{
		"https://example.com/?utm_source=news&q=go",
		"see https://example.com/?fbclid=abc.",
		`<a href="https://example.com/?utm_source=news">link</a>`,
	}
	for _, input := range inputs {
		once := StripText(input)
		if twice := StripText(once); twice != once {
			t.Fatalf("StripText(%q) = %q, then %q", input, once, twice)
		}
	}
}

func FuzzStripText(f *testing.F) {
	seeds := []string{
		"https://example.com/?utm_source=news&q=go",
		"https://example.com/?fbclid=abc#part",
		"look at https://example.com/?gclid=1, ok",
		`<a href="https://example.com/?utm_source=news&amp;q=go">x</a>`,
		"no links",
		"https://",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		stripped := StripText(text)
		if twice := StripText(stripped); twice != stripped {
			t.Fatalf("StripText(%q) = %q, then %q", text, stripped, twice)
		}
	})
}
