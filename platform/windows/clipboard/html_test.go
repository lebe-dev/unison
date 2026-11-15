package clipboard

import (
	"bytes"
	"fmt"
	"regexp"
	"testing"
)

// windowsPayload is a CF_HTML payload as an application writes it: a header of
// fixed width offsets and a document with the fragment marked up.
const windowsPayload = "Version:0.9\r\n" +
	"StartHTML:0000000105\r\n" +
	"EndHTML:0000000185\r\n" +
	"StartFragment:0000000139\r\n" +
	"EndFragment:0000000151\r\n" +
	"<html><body>\r\n<!--StartFragment--><b>hello</b><!--EndFragment-->\r\n</body></html>"

// windowsDocument is the document range of windowsPayload.
const windowsDocument = "<html><body>\r\n<!--StartFragment--><b>hello</b><!--EndFragment-->\r\n</body></html>"

func TestBuildCFHTML(t *testing.T) {
	fragment := []byte("<i>привет</i>")
	document, err := buildCFHTML(fragment)
	if err != nil {
		t.Fatalf("buildCFHTML: %v", err)
	}
	header, body := parseCFHTMLHeader(document)
	if body != header.startHTML {
		t.Fatalf("body starts at %d, header starts the document at %d", body, header.startHTML)
	}
	if header.endHTML != len(document) {
		t.Fatalf("endHTML = %d, payload is %d bytes", header.endHTML, len(document))
	}
	start, end, ok := header.fragment(len(document))
	if !ok {
		t.Fatal("header carries no fragment range")
	}
	if !bytes.Equal(document[start:end], fragment) {
		t.Fatalf("fragment range holds %q, want %q", document[start:end], fragment)
	}
	if got := fragmentOf(document); !bytes.Equal(got, fragment) {
		t.Fatalf("fragmentOf = %q, want %q", got, fragment)
	}
}

func TestParseCFHTMLHeader(t *testing.T) {
	header, body := parseCFHTMLHeader([]byte(windowsPayload))
	want := cfHTMLHeader{startHTML: 105, endHTML: 185, startFragment: 139, endFragment: 151}
	if header != want {
		t.Fatalf("header = %+v, want %+v", header, want)
	}
	if want := len(windowsPayload) - len(windowsDocument); body != want {
		t.Fatalf("body starts at %d, want %d", body, want)
	}
}

func TestParseCFHTMLHeaderOfPlainHTML(t *testing.T) {
	payload := []byte(`<p style="color:red">bare</p>`)
	header, body := parseCFHTMLHeader(payload)
	if body != 0 {
		t.Fatalf("body starts at %d, want 0", body)
	}
	if _, _, ok := header.fragment(len(payload)); ok {
		t.Fatal("plain html reported a fragment range")
	}
}

func TestParseCFHTMLHeaderWithoutOffsets(t *testing.T) {
	payload := "Version:1.0\n" +
		"StartHTML:0\n" +
		"EndHTML:0\n" +
		"StartSelection:0\n" +
		"EndSelection:0\n" +
		"SourceURL:https://example.com\n" +
		"<html></html>"
	header, body := parseCFHTMLHeader([]byte(payload))
	if want := len(payload) - len("<html></html>"); body != want {
		t.Fatalf("body starts at %d, want %d", body, want)
	}
	if _, _, ok := header.document(len(payload)); ok {
		t.Fatal("zero document range reported as usable")
	}
}

func TestParseOffset(t *testing.T) {
	tests := []struct {
		value string
		want  int
		ok    bool
	}{
		{value: "0", want: 0, ok: true},
		{value: "0000000105", want: 105, ok: true},
		{value: "105", want: 105, ok: true},
		{value: " \t42 ", want: 42, ok: true},
		{value: ""},
		{value: "  "},
		{value: "-1"},
		{value: "0.9"},
		{value: "12345678901"},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
			got, ok := parseOffset([]byte(test.value))
			if ok != test.ok || got != test.want {
				t.Fatalf("parseOffset(%q) = %d, %t; want %d, %t", test.value, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestFragmentOf(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "payload without header",
			payload: "<p>bare</p>",
			want:    "<p>bare</p>",
		},
		{
			name:    "windows payload",
			payload: windowsPayload,
			want:    "<b>hello</b>",
		},
		{
			name:    "fragment beyond the payload",
			payload: setOffset(t, windowsPayload, "StartFragment", 1000),
			want:    windowsDocument,
		},
		{
			name:    "fragment and document beyond the payload",
			payload: setOffset(t, setOffset(t, windowsPayload, "StartFragment", 1000), "StartHTML", 2000),
			want:    windowsDocument,
		},
		{
			name: "header without a body",
			payload: "Version:0.9\r\n" +
				"StartHTML:0000000105\r\n" +
				"EndHTML:0000000185\r\n",
			want: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := string(fragmentOf([]byte(test.payload))); got != test.want {
				t.Fatalf("fragmentOf = %q, want %q", got, test.want)
			}
		})
	}
}

// setOffset rewrites a fixed width offset of a payload, which keeps the header
// length and the document position unchanged.
func setOffset(t *testing.T, payload, field string, value int) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^` + field + `:\d+`)
	if !pattern.MatchString(payload) {
		t.Fatalf("payload has no %s field", field)
	}
	return pattern.ReplaceAllString(payload, fmt.Sprintf("%s:%010d", field, value))
}
