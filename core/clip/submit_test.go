package clip

import (
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

func TestSubmitRecordsAndAnnounces(t *testing.T) {
	f := newFixture(t)

	content := ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte("hello")},
		{MIME: MIMETextHTML, Data: []byte("<b>hello</b>")},
	}}
	item, err := f.pipe.Submit(t.Context(), content)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if item.ID.IsZero() || item.Seq != 1 || item.Origin != testOrigin {
		t.Fatalf("item %s seq %d origin %s", item.ID, item.Seq, item.Origin)
	}
	if len(item.Representations) != 2 {
		t.Fatalf("got %d representations, want 2", len(item.Representations))
	}

	select {
	case announced := <-f.announce:
		if announced.ID != item.ID {
			t.Fatalf("announced item %s, want %s", announced.ID, item.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("item was not announced")
	}

	last, ok := f.pipe.Last()
	if !ok || last.ID != item.ID {
		t.Fatal("submitted item did not reach the history")
	}
}

func TestSubmitRejectsUnsupportedContent(t *testing.T) {
	f := newFixture(t)

	content := ports.Content{Representations: []ports.Representation{
		{MIME: "image/png", Data: []byte("png")},
	}}
	if _, err := f.pipe.Submit(t.Context(), content); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("submit error = %v, want ErrUnsupportedContent", err)
	}
	if _, ok := f.pipe.Last(); ok {
		t.Fatal("unsupported content reached the history")
	}
}

func TestSubmitWhilePaused(t *testing.T) {
	f := newFixture(t)

	if err := f.pipe.Pause(t.Context(), time.Time{}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	content := ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte("hello")},
	}}
	if _, err := f.pipe.Submit(t.Context(), content); !errors.Is(err, ErrPaused) {
		t.Fatalf("submit error = %v, want ErrPaused", err)
	}
	select {
	case item := <-f.announce:
		t.Fatalf("paused pipeline announced %s", item.ID)
	default:
	}
}
