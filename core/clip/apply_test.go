package clip

import (
	"errors"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/model"
)

func TestApplyWritesClipboard(t *testing.T) {
	f := newFixture(t)

	item := textItem("hello", "<i>hello</i>")
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("clipboard was not written")
	}
	if len(content.Representations) != 2 {
		t.Fatalf("got %d representations, want 2", len(content.Representations))
	}
	if content.Representations[0].MIME != MIMETextPlain || string(content.Representations[0].Data) != "hello" {
		t.Fatalf("first representation is %s %q", content.Representations[0].MIME, content.Representations[0].Data)
	}
	if content.Representations[1].MIME != MIMETextHTML || string(content.Representations[1].Data) != "<i>hello</i>" {
		t.Fatalf("second representation is %s %q", content.Representations[1].MIME, content.Representations[1].Data)
	}

	last, ok := f.pipe.Last()
	if !ok || last.ID != item.ID {
		t.Fatal("applied item is not the most recent one")
	}
}

func TestApplyFollowsPolicy(t *testing.T) {
	f := newFixtureWith(t, func(opts *Options) {
		opts.Policy = fixedPolicy{decision: DecisionSkip}
	})

	if err := f.pipe.Apply(t.Context(), testPeer, textItem("hello", "")); err != nil {
		t.Fatalf("apply item: %v", err)
	}
	if _, ok := f.board.LastWrite(); ok {
		t.Fatal("clipboard was written for a skipped item")
	}
	if _, ok := f.pipe.Last(); ok {
		t.Fatal("skipped item reached the history")
	}
}

func TestApplyReportsPolicyError(t *testing.T) {
	want := errors.New("policy failed")
	f := newFixtureWith(t, func(opts *Options) {
		opts.Policy = fixedPolicy{err: want}
	})

	if err := f.pipe.Apply(t.Context(), testPeer, textItem("hello", "")); !errors.Is(err, want) {
		t.Fatalf("got error %v, want %v", err, want)
	}
	if _, ok := f.board.LastWrite(); ok {
		t.Fatal("clipboard was written after a policy error")
	}
}

func TestApplyRejectsContentWithoutInlineText(t *testing.T) {
	tests := []struct {
		name string
		item model.ClipItem
	}{
		{
			name: "image",
			item: model.ClipItem{Kind: model.KindImage, ID: model.ClipID{0x07}},
		},
		{
			name: "no representations",
			item: model.ClipItem{Kind: model.KindText, ID: model.ClipID{0x07}},
		},
		{
			name: "no inline payload",
			item: model.ClipItem{
				ID:   model.ClipID{0x07},
				Kind: model.KindText,
				Representations: []model.Representation{
					{MIME: MIMETextPlain, Size: 12},
				},
			},
		},
		{
			name: "above the inline limit",
			item: model.ClipItem{
				ID:   model.ClipID{0x07},
				Kind: model.KindText,
				Representations: []model.Representation{
					inlineRepresentation(MIMETextPlain, strings.Repeat("a", MaxInlineText+1)),
				},
			},
		},
		{
			name: "too many representations",
			item: model.ClipItem{
				ID:              model.ClipID{0x07},
				Kind:            model.KindText,
				Representations: make([]model.Representation, maxRepresentations+1),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)

			err := f.pipe.Apply(t.Context(), testPeer, test.item)
			if !errors.Is(err, ErrUnsupportedContent) {
				t.Fatalf("got error %v, want %v", err, ErrUnsupportedContent)
			}
			if _, ok := f.board.LastWrite(); ok {
				t.Fatal("clipboard was written for unsupported content")
			}
		})
	}
}

func TestApplyRequiresPeer(t *testing.T) {
	f := newFixture(t)

	err := f.pipe.Apply(t.Context(), model.DeviceID{}, textItem("hello", ""))
	if err == nil {
		t.Fatal("apply without a peer must fail")
	}
}

func TestApplyWritesOnlyTextRepresentations(t *testing.T) {
	f := newFixture(t)

	item := textItem("hello", "")
	item.Representations = append(item.Representations, model.Representation{
		MIME:   "image/png",
		Size:   3,
		Hash:   hashOf([]byte("png")),
		Inline: []byte("png"),
	})
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("clipboard was not written")
	}
	if len(content.Representations) != 1 || content.Representations[0].MIME != MIMETextPlain {
		t.Fatalf("clipboard got %d representations", len(content.Representations))
	}
}

func TestApplyWritesACopyOfThePayload(t *testing.T) {
	f := newFixture(t)

	item := textItem("hello", "")
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	item.Representations[0].Inline[0] = 'j'

	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("clipboard was not written")
	}
	if got := string(content.Representations[0].Data); got != "hello" {
		t.Fatalf("clipboard holds %q after the item changed", got)
	}
}

func TestApplyNamesTheItemInTheClipboard(t *testing.T) {
	f := newFixture(t)

	item := textItem("hello", "")
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("clipboard was not written")
	}
	if content.ItemID != item.ID.String() {
		t.Fatalf("clipboard names item %q, want %q", content.ItemID, item.ID.String())
	}
}

func TestApplyHistoryWritesClipboard(t *testing.T) {
	f := newFixture(t)
	item := textItem("recorded", "")

	if err := f.pipe.ApplyHistory(t.Context(), item); err != nil {
		t.Fatalf("apply the recorded item: %v", err)
	}

	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("clipboard was not written")
	}
	if len(content.Representations) != 1 || string(content.Representations[0].Data) != "recorded" {
		t.Fatal("the clipboard does not hold the item")
	}
	if content.ItemID != item.ID.String() {
		t.Fatalf("clipboard names item %q, want %q", content.ItemID, item.ID.String())
	}
	if _, ok := f.pipe.Last(); ok {
		t.Fatal("applying a recorded item recorded another one")
	}
}

func TestApplyHistoryRejectsUnsupportedContent(t *testing.T) {
	f := newFixture(t)

	err := f.pipe.ApplyHistory(t.Context(), model.ClipItem{Kind: model.KindText, ID: model.ClipID{0x07}})
	if !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("got error %v, want %v", err, ErrUnsupportedContent)
	}
	if _, ok := f.board.LastWrite(); ok {
		t.Fatal("clipboard was written for unsupported content")
	}
}
