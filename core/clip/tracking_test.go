package clip

import (
	"testing"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

func TestSubmitStripsTracking(t *testing.T) {
	f := newFixtureWith(t, func(opts *Options) {
		opts.StripTracking = true
	})

	content := ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte("https://example.com/page?utm_source=news&q=go")},
		{MIME: MIMETextHTML, Data: []byte(`<a href="https://example.com/page?utm_source=news&q=go">link</a>`)},
	}}
	item, err := f.pipe.Submit(t.Context(), content)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertInline(t, item.Representations, MIMETextPlain, "https://example.com/page?q=go")
	assertInline(t, item.Representations, MIMETextHTML, `<a href="https://example.com/page?q=go">link</a>`)
}

func TestSubmitKeepsTrackingWhenDisabled(t *testing.T) {
	f := newFixture(t)

	text := "https://example.com/page?utm_source=news&q=go"
	item, err := f.pipe.Submit(t.Context(), ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte(text)},
	}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertInline(t, item.Representations, MIMETextPlain, text)
}

func TestApplyStripsTracking(t *testing.T) {
	f := newFixtureWith(t, func(opts *Options) {
		opts.StripTracking = true
	})

	item := textItem(
		"https://example.com/page?fbclid=abc&q=go",
		`<a href="https://example.com/page?fbclid=abc&q=go">link</a>`,
	)
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}
	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("clipboard was not written")
	}
	if got := string(content.Representations[0].Data); got != "https://example.com/page?q=go" {
		t.Fatalf("plain text is %q", got)
	}
	if got := string(content.Representations[1].Data); got != `<a href="https://example.com/page?q=go">link</a>` {
		t.Fatalf("html is %q", got)
	}
}

func TestSetStripTrackingAppliesAtOnce(t *testing.T) {
	f := newFixture(t)

	f.pipe.SetStripTracking(true)
	item, err := f.pipe.Submit(t.Context(), ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte("https://example.com/?utm_source=news")},
	}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertInline(t, item.Representations, MIMETextPlain, "https://example.com/")

	f.pipe.SetStripTracking(false)
	if f.pipe.StripTracking() {
		t.Fatal("the setting is still on")
	}
}

// assertInline checks the payload of one representation.
func assertInline(t *testing.T, reps []model.Representation, mime, want string) {
	t.Helper()
	for _, rep := range reps {
		if rep.MIME == mime {
			if got := string(rep.Inline); got != want {
				t.Fatalf("%s is %q; want %q", mime, got, want)
			}
			return
		}
	}
	t.Fatalf("no %s representation", mime)
}
