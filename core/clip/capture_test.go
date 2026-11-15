package clip

import (
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

func TestSensitiveContentIsDropped(t *testing.T) {
	f := newFixture(t)

	sensitive := textSnapshot("secret", "")
	sensitive.Sensitive = true
	f.emit(sensitive)

	f.expectNoItem()
	if _, ok := f.pipe.Last(); ok {
		t.Fatal("sensitive content reached the history")
	}
}

func TestSensitiveContentDiscardsPendingChange(t *testing.T) {
	f := newFixture(t)

	draft := textSnapshot("draft", "")
	f.emit(draft)

	sensitive := textSnapshot("secret", "")
	sensitive.Sensitive = true
	f.emit(sensitive)

	f.expectNoItem()
	if _, ok := f.pipe.Last(); ok {
		t.Fatal("pending content was captured after a sensitive change")
	}
}

func TestOwnContentIsDropped(t *testing.T) {
	f := newFixture(t)

	snapshot := textSnapshot("mine", "")
	snapshot.OwnedByUs = true
	f.emit(snapshot)

	f.expectNoItem()
}

func TestEchoIsNotCaptured(t *testing.T) {
	f := newFixture(t)

	if err := f.pipe.Apply(t.Context(), testPeer, textItem("echo", "")); err != nil {
		t.Fatalf("apply item: %v", err)
	}
	if _, ok := f.board.LastWrite(); !ok {
		t.Fatal("clipboard was not written")
	}

	f.emit(textSnapshot("echo", ""))
	f.expectNoItem()

	f.clock.Advance(DefaultEchoWindow + step)
	f.emit(textSnapshot("echo", ""))
	if item := f.waitItem(); item.Seq != 1 {
		t.Fatalf("captured seq %d, want 1", item.Seq)
	}
}

func TestDebounceMergesTextRepresentations(t *testing.T) {
	f := newFixture(t)

	f.board.Emit(textSnapshot("plain", ""))
	f.board.Emit(textSnapshot("plain", "<b>plain</b>"))
	f.deliver()

	item := f.waitItem()
	if len(item.Representations) != 2 {
		t.Fatalf("got %d representations, want 2", len(item.Representations))
	}
	plain, html := item.Representations[0], item.Representations[1]
	if plain.MIME != MIMETextPlain || string(plain.Inline) != "plain" {
		t.Fatalf("first representation is %s %q", plain.MIME, plain.Inline)
	}
	if html.MIME != MIMETextHTML || string(html.Inline) != "<b>plain</b>" {
		t.Fatalf("second representation is %s %q", html.MIME, html.Inline)
	}
	if plain.Size != uint64(len(plain.Inline)) || html.Size != uint64(len(html.Inline)) {
		t.Fatal("representation size does not match the payload")
	}
	if plain.Hash != inlineRepresentation(MIMETextPlain, "plain").Hash {
		t.Fatal("plain representation hash does not match the payload")
	}
	if html.Hash != inlineRepresentation(MIMETextHTML, "<b>plain</b>").Hash {
		t.Fatal("html representation hash does not match the payload")
	}

	f.expectNoItem()
}

func TestOversizedContentIsSkipped(t *testing.T) {
	f := newFixture(t)

	f.emit(textSnapshot(strings.Repeat("a", MaxInlineText+1), ""))

	f.expectNoItem()
	if _, ok := f.pipe.Last(); ok {
		t.Fatal("oversized content reached the history")
	}
}

func TestMaxInlineContentIsCaptured(t *testing.T) {
	f := newFixture(t)

	value := strings.Repeat("a", MaxInlineText)
	f.emit(textSnapshot(value, ""))

	if item := f.waitItem(); string(item.Representations[0].Inline) != value {
		t.Fatal("captured payload differs from the clipboard content")
	}
}

func TestNonTextContentIsSkipped(t *testing.T) {
	f := newFixture(t)

	f.emit(ports.Snapshot{Content: ports.Content{Representations: []ports.Representation{
		{MIME: MIMETextPlain, Data: []byte{0xff, 0xfe}},
		{MIME: "image/png", Data: []byte("png")},
	}}})

	f.expectNoItem()
}

func TestCapturedItemCarriesMetadata(t *testing.T) {
	f := newFixture(t)

	f.emit(textSnapshot("hello", ""))
	item := f.waitItem()

	if item.ID.IsZero() {
		t.Fatal("item has no id")
	}
	if item.ID[6]>>4 != 7 {
		t.Fatalf("item id is not a UUIDv7")
	}
	if item.Origin != testOrigin {
		t.Fatalf("origin is %s, want %s", item.Origin, testOrigin)
	}
	if item.Kind != model.KindText || item.Source != model.SourceClipboard {
		t.Fatalf("kind %s source %s", item.Kind, item.Source)
	}
	if item.ExpiresAt.Sub(item.CreatedAt) != DefaultTextTTL {
		t.Fatalf("ttl is %s, want %s", item.ExpiresAt.Sub(item.CreatedAt), DefaultTextTTL)
	}
	if item.Representations[0].Hash != inlineRepresentation(MIMETextPlain, "hello").Hash {
		t.Fatal("representation hash does not match the payload")
	}
}

func TestSequenceIsMonotonicAfterRestart(t *testing.T) {
	f := newFixture(t)

	f.emit(textSnapshot("one", ""))
	if item := f.waitItem(); item.Seq != 1 {
		t.Fatalf("first seq %d, want 1", item.Seq)
	}

	f.emit(textSnapshot("two", ""))
	if item := f.waitItem(); item.Seq != 2 {
		t.Fatalf("second seq %d, want 2", item.Seq)
	}

	f.stop()
	f.start()

	f.emit(textSnapshot("three", ""))
	if item := f.waitItem(); item.Seq != 3 {
		t.Fatalf("seq after restart %d, want 3", item.Seq)
	}
}

func TestLastReturnsMostRecentItem(t *testing.T) {
	f := newFixture(t)

	if _, ok := f.pipe.Last(); ok {
		t.Fatal("empty history has an item")
	}

	f.emit(textSnapshot("one", ""))
	first := f.waitItem()
	f.emit(textSnapshot("two", ""))
	second := f.waitItem()

	last, ok := f.pipe.Last()
	if !ok {
		t.Fatal("history is empty")
	}
	if last.ID != second.ID || last.ID == first.ID {
		t.Fatal("history does not hold the most recent item")
	}
}
