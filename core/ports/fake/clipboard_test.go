package fake

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

func TestClipboardWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clipboard := NewClipboard()

	events, err := clipboard.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	want := ports.Snapshot{
		Content:   ports.Content{Representations: []ports.Representation{{MIME: "text/plain", Data: []byte("hello")}}},
		Sensitive: true,
		OwnedByUs: true,
	}
	clipboard.Emit(want)

	select {
	case got := <-events:
		if got.Sensitive != want.Sensitive || got.OwnedByUs != want.OwnedByUs {
			t.Fatalf("snapshot flags: got %+v, want %+v", got, want)
		}
		if len(got.Content.Representations) != 1 || got.Content.Representations[0].MIME != "text/plain" ||
			string(got.Content.Representations[0].Data) != "hello" {
			t.Fatalf("snapshot content: got %+v", got.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("no snapshot received")
	}

	cancel()
	waitClosed(t, events)
}

func TestClipboardWrite(t *testing.T) {
	ctx := context.Background()
	clipboard := NewClipboard()

	if _, ok := clipboard.LastWrite(); ok {
		t.Fatal("clipboard reports a write before any Write call")
	}

	content := ports.Content{Representations: []ports.Representation{{MIME: "text/plain", Data: []byte("hi")}}}
	if err := clipboard.Write(ctx, content); err != nil {
		t.Fatalf("write: %v", err)
	}
	content.Representations[0].Data[0] = 'x'

	last, ok := clipboard.LastWrite()
	if !ok {
		t.Fatal("no write recorded")
	}
	if !slices.Equal(last.Representations[0].Data, []byte("hi")) {
		t.Fatalf("recorded data: got %q, want %q", last.Representations[0].Data, "hi")
	}
}

func TestClipboardWatchersAreIndependent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clipboard := NewClipboard()

	first, err := clipboard.Watch(ctx)
	if err != nil {
		t.Fatalf("watch first: %v", err)
	}
	second, err := clipboard.Watch(ctx)
	if err != nil {
		t.Fatalf("watch second: %v", err)
	}

	clipboard.Emit(ports.Snapshot{})

	for i, events := range []<-chan ports.Snapshot{first, second} {
		select {
		case <-events:
		case <-time.After(time.Second):
			t.Fatalf("watcher %d received nothing", i)
		}
	}
}

func waitClosed(t *testing.T, events <-chan ports.Snapshot) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("watch channel was not closed")
		}
	}
}
