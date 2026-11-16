package daemon

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/share"
)

// TestLogEventCarriesNoPayload checks that the daemon describes an item by its
// identifier, kind, size and types, without the payload or the file name.
func TestLogEventCarriesNoPayload(t *testing.T) {
	const marker = "logaudit-5c02d1"

	var output bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))

	item := model.ClipItem{
		Kind: model.KindText,
		Representations: []model.Representation{
			{MIME: "text/plain", Size: 4, Inline: []byte(marker + "-text")},
			{MIME: "text/html", Size: 4, Inline: []byte(marker + "-html")},
			{MIME: "application/octet-stream", Size: 4, FileName: marker + "-name", Inline: []byte(marker + "-file")},
		},
	}
	logEvent(log, node.Event{Kind: node.EventClipboard, Item: item})
	logEvent(log, node.Event{Kind: node.EventClipboard, Local: true, Item: item})
	logEvent(log, node.Event{Kind: node.EventTransfer, Transfer: share.Transfer{
		Files: []share.File{{Name: marker + "-share", Size: 4}},
	}})

	text := output.String()
	if !strings.Contains(text, "clipboard item") {
		t.Fatalf("the item was not logged: %s", text)
	}
	if !strings.Contains(text, "transfer") {
		t.Fatalf("the transfer was not logged: %s", text)
	}
	if strings.Contains(text, marker) {
		t.Fatalf("the log carries payload: %s", text)
	}
}
