package nodetest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/share"
)

// The markers are unique strings planted in the payloads of the test. A log
// line that carries payload holds one of them.
const (
	markerPrefix   = "logaudit-b41f7c"
	textMarker     = markerPrefix + "-text"
	htmlMarker     = markerPrefix + "-html"
	imageMarker    = markerPrefix + "-image"
	fileNameMarker = markerPrefix + "-file-name"
	fileMarker     = markerPrefix + "-file-content"
)

// logBuffer collects the log output of every node.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write stores one chunk of the log output.
func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns the log output collected so far.
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestLogsCarryNoPayload moves text, HTML, an image and a file between two
// devices and checks that the payload never reaches the logs of the nodes.
func TestLogsCarryNoPayload(t *testing.T) {
	logs := &logBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := New(t, WithLogger(logger))
	alpha := h.NewNode("alpha")
	beta := h.NewNode("beta")
	h.Pair(alpha, beta)

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout())
	defer cancel()

	alpha.Submit(textMarker)
	beta.WaitText(textMarker)

	html := submit(t, alpha, ports.Content{Representations: []ports.Representation{
		{MIME: clip.MIMETextPlain, Data: []byte("formatted text")},
		{MIME: clip.MIMETextHTML, Data: []byte(`<b data-audit="` + htmlMarker + `">formatted text</b>`)},
	}})
	waitItem(t, beta, html.ID)

	picture := submit(t, alpha, ports.Content{Representations: []ports.Representation{
		{MIME: clip.MIMEImagePNG, Data: pngWithText(t, imageMarker)},
	}})
	waitItem(t, beta, picture.ID)

	path := filepath.Join(t.TempDir(), fileNameMarker+".txt")
	content := append([]byte(fileMarker+"\n"), bytes.Repeat([]byte("file line\n"), 16<<10)...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	file, err := alpha.Clip().Submit(ctx, ports.Content{Representations: []ports.Representation{
		{MIME: clip.MIMEUnisonFile, FilePath: path},
	}})
	if err != nil {
		t.Fatalf("submit the file: %v", err)
	}
	waitItem(t, beta, file.ID)

	// The share path logs a saved file, which is where a file name could reach
	// the log, so it gets a name and content of its own.
	ids, err := alpha.Share().SendPaths(ctx, []string{path}, []model.DeviceID{beta.DeviceID()})
	if err != nil {
		t.Fatalf("share the file: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("the share created %d transfers, want one", len(ids))
	}
	done := waitIncomingDone(t, beta, alpha.DeviceID())
	if len(done.Paths) != 1 {
		t.Fatalf("the transfer saved %d files, want one", len(done.Paths))
	}

	output := logs.String()
	if lines := strings.Count(output, "\n"); lines < 2 {
		t.Fatalf("the capture holds %d log lines, so the check proves nothing", lines)
	}
	for _, message := range []string{"applied clip item", "saved a shared file"} {
		if !strings.Contains(output, message) {
			t.Fatalf("the capture holds no %q line, so the check proves nothing", message)
		}
	}
	for _, marker := range []string{textMarker, htmlMarker, imageMarker, fileNameMarker, fileMarker} {
		for _, form := range markerForms(marker) {
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(line, form) {
					t.Fatalf("a log line carries the payload marker %q: %s", marker, line)
				}
			}
		}
	}
}

// markerForms returns the plain and the encoded forms of a marker, because a
// payload logged as bytes is base64 or hex encoded.
func markerForms(marker string) []string {
	raw := []byte(marker)
	return []string{
		marker,
		base64.StdEncoding.EncodeToString(raw),
		base64.RawStdEncoding.EncodeToString(raw),
		base64.URLEncoding.EncodeToString(raw),
		hex.EncodeToString(raw),
	}
}

// submit sends content through the local path of the pipeline.
func submit(t *testing.T, n *Node, content ports.Content) model.ClipItem {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), n.harness.timeout)
	defer cancel()
	item, err := n.Clip().Submit(ctx, content)
	if err != nil {
		t.Fatalf("node %s: submit content: %v", n.name, err)
	}
	return item
}

// waitItem waits until the node reports an item with the identifier.
func waitItem(t *testing.T, n *Node, id model.ClipID) {
	t.Helper()

	deadline := time.Now().Add(n.harness.timeout)
	for {
		for _, event := range n.Events() {
			if event.Item.ID != id {
				continue
			}
			if event.Kind == node.EventClipboard || event.Kind == node.EventClipDownload {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("node %s: no event for the item %s within %v (events: %s)",
				n.name, id, n.harness.timeout, n.summary())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitIncomingDone waits until the node finished an incoming transfer of the
// peer.
func waitIncomingDone(t *testing.T, n *Node, peer model.DeviceID) share.Transfer {
	t.Helper()

	deadline := time.Now().Add(n.harness.timeout)
	for {
		for _, transfer := range n.Share().Transfers() {
			if transfer.Direction != share.DirectionIncoming || transfer.Peer != peer {
				continue
			}
			if transfer.State == share.StateDone {
				return transfer
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("node %s: no finished transfer from %s within %v",
				n.name, peerLabel(peer), n.harness.timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pngWithText encodes a small image with a tEXt chunk, so the marker is part of
// the image payload.
func pngWithText(t *testing.T, text string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewGray(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatalf("encode the image: %v", err)
	}
	data := buffer.Bytes()
	iend := len(data) - 12
	chunk := textChunk("Comment", text)

	out := make([]byte, 0, len(data)+len(chunk))
	out = append(out, data[:iend]...)
	out = append(out, chunk...)
	return append(out, data[iend:]...)
}

// textChunk builds a PNG tEXt chunk.
func textChunk(keyword, text string) []byte {
	data := make([]byte, 0, len(keyword)+1+len(text))
	data = append(data, keyword...)
	data = append(data, 0)
	data = append(data, text...)

	chunk := make([]byte, 0, len(data)+12)
	chunk = binary.BigEndian.AppendUint32(chunk, uint32(len(data)))
	chunk = append(chunk, "tEXt"...)
	chunk = append(chunk, data...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}
