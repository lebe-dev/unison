package clip

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// memoryPayloads is a PayloadStore that keeps the payloads in memory.
type memoryPayloads struct {
	mu   sync.Mutex
	data map[[32]byte][]byte
}

// newMemoryPayloads creates an empty payload store.
func newMemoryPayloads() *memoryPayloads {
	return &memoryPayloads{data: make(map[[32]byte][]byte)}
}

var _ PayloadStore = (*memoryPayloads)(nil)

// Put stores the content read from src.
func (m *memoryPayloads) Put(_ context.Context, src io.ReaderAt, size int64) ([32]byte, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(io.NewSectionReader(src, 0, size), data); err != nil {
		return [32]byte{}, err
	}
	hash := blake3.Sum256(data)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[hash] = data
	return hash, nil
}

// Has reports whether the payload is stored.
func (m *memoryPayloads) Has(_ context.Context, hash [32]byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[hash]
	return ok, nil
}

// Open returns a reader over the payload.
func (m *memoryPayloads) Open(_ context.Context, hash [32]byte) (Payload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.data[hash]
	if !ok {
		return nil, errors.New("payload is missing")
	}
	return &memoryPayload{bytes.NewReader(data)}, nil
}

// get returns the stored payload and whether it exists.
func (m *memoryPayloads) get(hash [32]byte) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.data[hash]
	return data, ok
}

// memoryPayload reads a stored payload from memory.
type memoryPayload struct {
	*bytes.Reader
}

// Size returns the payload size in bytes.
func (p *memoryPayload) Size() int64 {
	return int64(p.Len())
}

// Close releases the payload.
func (p *memoryPayload) Close() error {
	return nil
}

// eventRecorder collects the pipeline events.
type eventRecorder struct {
	events chan Event
}

// newEventRecorder creates a recorder with room for the test events.
func newEventRecorder() *eventRecorder {
	return &eventRecorder{events: make(chan Event, 16)}
}

var _ EventSink = (*eventRecorder)(nil)

// Report stores one event.
func (r *eventRecorder) Report(_ context.Context, event Event) {
	r.events <- event
}

// wait returns the next event of the given kind.
func (r *eventRecorder) wait(t *testing.T, kind EventKind) Event {
	t.Helper()

	timeout := time.After(2 * time.Second)
	for {
		select {
		case event := <-r.events:
			if event.Kind == kind {
				return event
			}
		case <-timeout:
			t.Fatalf("no %s event", kind)
		}
	}
}

// fixedPlanner returns the same outcome for every item.
type fixedPlanner struct {
	outcome Outcome
	err     error
}

func (p fixedPlanner) Plan(context.Context, model.DeviceID, model.ClipItem) (Outcome, error) {
	return p.outcome, p.err
}

// imageBytes encodes a small test image in the given format.
func imageBytes(t *testing.T, format string, width, height int) []byte {
	t.Helper()

	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			source.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 0x40, A: 0xff})
		}
	}

	var buffer bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buffer, source)
	case "jpeg":
		err = jpeg.Encode(&buffer, source, nil)
	case "tiff":
		err = tiff.Encode(&buffer, source, nil)
	case "bmp":
		err = bmp.Encode(&buffer, source)
	default:
		t.Fatalf("unknown image format %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s image: %v", format, err)
	}
	return buffer.Bytes()
}

// imageSnapshot builds a clipboard snapshot with one image.
func imageSnapshot(format string, data []byte) ports.Snapshot {
	return ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: "image/" + format, Data: data}},
	}}
}

// fileSnapshot builds a clipboard snapshot with one file.
func fileSnapshot(path string) ports.Snapshot {
	return ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: MIMEUnisonFile, FilePath: path}},
	}}
}

// blobRepresentation builds a representation whose payload is stored by hash.
func blobRepresentation(mime string, data []byte) model.Representation {
	return model.Representation{
		MIME: mime,
		Size: uint64(len(data)),
		Hash: blake3.Sum256(data),
	}
}

// TestImageIsNormalizedToPNG checks the conversion and the second
// representation of the original format.
func TestImageIsNormalizedToPNG(t *testing.T) {
	tests := []struct {
		format string
		mime   string
	}{
		{format: "jpeg", mime: "image/jpeg"},
		{format: "tiff", mime: "image/tiff"},
		{format: "bmp", mime: "image/bmp"},
	}

	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			f := newFixtureWith(t, func(opts *Options) {
				opts.Payloads = newMemoryPayloads()
			})

			f.emit(imageSnapshot(test.format, imageBytes(t, test.format, 16, 16)))
			item := f.waitItem()

			if item.Kind != model.KindImage {
				t.Fatalf("kind is %s, want image", item.Kind)
			}
			if got := item.Representations[0].MIME; got != MIMEImagePNG {
				t.Fatalf("first representation is %s, want %s", got, MIMEImagePNG)
			}
			if item.ExpiresAt.Sub(item.CreatedAt) != DefaultImageTTL {
				t.Fatalf("ttl is %s, want %s", item.ExpiresAt.Sub(item.CreatedAt), DefaultImageTTL)
			}

			pngData := representationData(t, f.pipe.payloads, item.Representations[0])
			decoded, err := png.Decode(bytes.NewReader(pngData))
			if err != nil {
				t.Fatalf("decode the stored png: %v", err)
			}
			if decoded.Bounds() != image.Rect(0, 0, 16, 16) {
				t.Fatalf("png bounds are %v, want 16x16", decoded.Bounds())
			}

			// The original format is kept only while it stays below twice
			// the size of the PNG.
			originalSize := len(imageBytes(t, test.format, 16, 16))
			if originalSize < 2*len(pngData) {
				if len(item.Representations) != 2 {
					t.Fatalf("got %d representations, want the original format too", len(item.Representations))
				}
				if got := item.Representations[1].MIME; got != test.mime {
					t.Fatalf("second representation is %s, want %s", got, test.mime)
				}
				return
			}
			if len(item.Representations) != 1 {
				t.Fatalf("the original format of %d bytes is kept next to a %d byte png", originalSize, len(pngData))
			}
		})
	}
}

// TestPNGImageIsNotConverted keeps a PNG source as it is.
func TestPNGImageIsNotConverted(t *testing.T) {
	f := newFixture(t)

	data := imageBytes(t, "png", 8, 8)
	f.emit(imageSnapshot("png", data))
	item := f.waitItem()

	if len(item.Representations) != 1 {
		t.Fatalf("got %d representations, want one", len(item.Representations))
	}
	if item.Representations[0].MIME != MIMEImagePNG || !bytes.Equal(item.Representations[0].Inline, data) {
		t.Fatal("the png payload changed")
	}
}

// TestAbsurdImageIsSkipped rejects an image above the pixel limit.
func TestAbsurdImageIsSkipped(t *testing.T) {
	f := newFixture(t)

	source := image.NewGray(image.Rect(0, 0, MaxImageSide+1, 1))
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	f.emit(imageSnapshot("png", buffer.Bytes()))

	f.expectNoItem()
}

// TestCorruptImageIsSkipped drops an image that cannot be decoded.
func TestCorruptImageIsSkipped(t *testing.T) {
	f := newFixture(t)

	f.emit(imageSnapshot("png", []byte("not an image")))
	f.expectNoItem()
}

// TestLargeTextGoesToThePayloadStore checks the inline threshold of text.
func TestLargeTextGoesToThePayloadStore(t *testing.T) {
	store := newMemoryPayloads()
	f := newFixtureWith(t, func(opts *Options) {
		opts.Payloads = store
	})

	value := bytes.Repeat([]byte("a"), MaxInlineText+10)
	f.emit(textSnapshot(string(value), ""))
	item := f.waitItem()

	rep := item.Representations[0]
	if rep.MIME != MIMETextPlain {
		t.Fatalf("representation is %s", rep.MIME)
	}
	if len(rep.Inline) != 0 {
		t.Fatal("the oversized text travels inline")
	}
	if rep.Hash != blake3.Sum256(value) {
		t.Fatal("the text hash differs from the payload")
	}
	stored, ok := store.get(rep.Hash)
	if !ok || !bytes.Equal(stored, value) {
		t.Fatal("the text is not in the payload store")
	}
}

// TestSmallFileIsInline keeps a small file inside the item.
func TestSmallFileIsInline(t *testing.T) {
	f := newFixtureWith(t, func(opts *Options) {
		opts.Payloads = newMemoryPayloads()
	})

	path := writeTempFile(t, "note.txt", []byte("a small file"))
	f.emit(fileSnapshot(path))
	item := f.waitItem()

	if item.Kind != model.KindFiles {
		t.Fatalf("kind is %s, want files", item.Kind)
	}
	if item.ExpiresAt.Sub(item.CreatedAt) != DefaultFilesTTL {
		t.Fatalf("ttl is %s, want %s", item.ExpiresAt.Sub(item.CreatedAt), DefaultFilesTTL)
	}

	rep := item.Representations[0]
	if rep.MIME != MIMEUnisonFile || rep.FileName != "note.txt" {
		t.Fatalf("representation is %s %q", rep.MIME, rep.FileName)
	}
	if string(rep.Inline) != "a small file" {
		t.Fatalf("inline payload is %q", rep.Inline)
	}
	if rep.MTime.IsZero() {
		t.Fatal("the file modification time is not set")
	}
}

// TestLargeFileGoesToThePayloadStore keeps only the hash of a large file in the
// item.
func TestLargeFileGoesToThePayloadStore(t *testing.T) {
	store := newMemoryPayloads()
	f := newFixtureWith(t, func(opts *Options) {
		opts.Payloads = store
	})

	data := bytes.Repeat([]byte("0123456789"), MaxInlineText)
	path := writeTempFile(t, "big.bin", data)
	f.emit(fileSnapshot(path))
	item := f.waitItem()

	rep := item.Representations[0]
	if len(rep.Inline) != 0 {
		t.Fatal("the large file travels inline")
	}
	if rep.Size != uint64(len(data)) || rep.Hash != blake3.Sum256(data) {
		t.Fatalf("representation is %d bytes with hash %x", rep.Size, rep.Hash)
	}
	stored, ok := store.get(rep.Hash)
	if !ok || !bytes.Equal(stored, data) {
		t.Fatal("the file is not in the payload store")
	}
}

// TestIncomingMetadataItem checks the metadata-only outcome.
func TestIncomingMetadataItem(t *testing.T) {
	events := newEventRecorder()
	f := newFixtureWith(t, func(opts *Options) {
		opts.Events = events
		opts.Planner = fixedPlanner{outcome: Outcome{Action: ActionMetadata}}
	})

	item := textItem("kept in the history", "")
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	if _, ok := f.board.LastWrite(); ok {
		t.Fatal("the metadata item reached the clipboard")
	}
	last, ok := f.pipe.Last()
	if !ok || last.ID != item.ID {
		t.Fatal("the metadata item is not in the history")
	}
	event := events.wait(t, EventDownload)
	if event.Peer != testPeer || event.Item.ID != item.ID {
		t.Fatalf("event is for %s from %s", event.Item.ID, event.Peer)
	}
}

// TestIncomingConfirmItem checks the confirmation outcome.
func TestIncomingConfirmItem(t *testing.T) {
	events := newEventRecorder()
	f := newFixtureWith(t, func(opts *Options) {
		opts.Events = events
		opts.Planner = fixedPlanner{outcome: Outcome{Action: ActionConfirm}}
	})

	item := textItem("ask first", "")
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}
	if _, ok := f.board.LastWrite(); ok {
		t.Fatal("the item reached the clipboard without confirmation")
	}
	if event := events.wait(t, EventApply); event.Item.ID != item.ID {
		t.Fatalf("event is for %s", event.Item.ID)
	}
}

// TestIncomingItemIsFetchedEagerly checks the eager download of a blob payload.
func TestIncomingItemIsFetchedEagerly(t *testing.T) {
	remote := newMemoryPayloads()
	local := newMemoryPayloads()
	value := []byte("a text kept in the blob store")

	ctx := t.Context()
	hash, err := remote.Put(ctx, bytes.NewReader(value), int64(len(value)))
	if err != nil {
		t.Fatalf("store payload: %v", err)
	}

	f := newFixtureWith(t, func(opts *Options) {
		opts.Payloads = local
		opts.Planner = fixedPlanner{outcome: Outcome{Action: ActionApply, Eager: true}}
		opts.Fetch = FetchFunc(func(ctx context.Context, want [32]byte) error {
			if want != hash {
				return errors.New("unexpected payload")
			}
			data, ok := remote.get(hash)
			if !ok {
				return errors.New("payload is missing")
			}
			_, err := local.Put(ctx, bytes.NewReader(data), int64(len(data)))
			return err
		})
	})

	item := textItem("", "")
	item.Representations = []model.Representation{blobRepresentation(MIMETextPlain, value)}
	if err := f.pipe.Apply(ctx, testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("the fetched item did not reach the clipboard")
	}
	if got := string(content.Representations[0].Data); got != string(value) {
		t.Fatalf("clipboard holds %q", got)
	}
}

// TestIncomingBlobItemWithoutEagerIsReported checks that a missing payload is
// not fetched without the eager flag.
func TestIncomingBlobItemWithoutEagerIsReported(t *testing.T) {
	events := newEventRecorder()
	f := newFixtureWith(t, func(opts *Options) {
		opts.Payloads = newMemoryPayloads()
		opts.Events = events
		opts.Planner = fixedPlanner{outcome: Outcome{Action: ActionApply}}
		opts.Fetch = FetchFunc(func(context.Context, [32]byte) error {
			t.Fatal("the payload was fetched without the eager flag")
			return nil
		})
	})

	item := textItem("", "")
	item.Representations = []model.Representation{blobRepresentation(MIMETextPlain, []byte("remote"))}
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}
	if _, ok := f.board.LastWrite(); ok {
		t.Fatal("an item without its payload reached the clipboard")
	}
	events.wait(t, EventDownload)
}

// TestBinaryEchoIsIgnored checks the echo guard for image payloads.
func TestBinaryEchoIsIgnored(t *testing.T) {
	f := newFixtureWith(t, func(opts *Options) {
		opts.Planner = fixedPlanner{outcome: Outcome{Action: ActionApply, Eager: true}}
	})

	data := imageBytes(t, "png", 8, 8)
	item := model.ClipItem{
		ID:              model.ClipID{0x09},
		Origin:          testPeer,
		Seq:             7,
		CreatedAt:       time.Date(2026, 11, 15, 11, 0, 0, 0, time.UTC),
		ExpiresAt:       time.Date(2026, 11, 16, 11, 0, 0, 0, time.UTC),
		Kind:            model.KindImage,
		Representations: []model.Representation{{MIME: MIMEImagePNG, Size: uint64(len(data)), Hash: blake3.Sum256(data), Inline: data}},
	}
	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	f.emit(imageSnapshot("png", data))
	f.expectNoItem()

	f.clock.Advance(DefaultEchoWindow + step)
	f.emit(imageSnapshot("png", data))
	if captured := f.waitItem(); captured.Kind != model.KindImage {
		t.Fatalf("captured kind is %s", captured.Kind)
	}
}

// TestIncomingFileIsSavedToDownloads checks the file outcome and the safe file
// name.
func TestIncomingFileIsSavedToDownloads(t *testing.T) {
	downloads := filepath.Join(t.TempDir(), "downloads")
	f := newFixtureWith(t, func(opts *Options) {
		opts.DownloadsDir = downloads
		opts.Planner = fixedPlanner{outcome: Outcome{Action: ActionApply, Eager: true}}
	})

	data := []byte("content of the incoming file")
	item := textItem("", "")
	item.Kind = model.KindFiles
	item.Representations = []model.Representation{{
		MIME:     MIMEUnisonFile,
		Size:     uint64(len(data)),
		Hash:     blake3.Sum256(data),
		Inline:   data,
		FileName: "../escape.txt",
	}}

	if err := f.pipe.Apply(t.Context(), testPeer, item); err != nil {
		t.Fatalf("apply item: %v", err)
	}

	content, ok := f.board.LastWrite()
	if !ok {
		t.Fatal("the item did not reach the clipboard")
	}
	if len(content.Representations) != 1 {
		t.Fatalf("clipboard got %d representations", len(content.Representations))
	}
	target := content.Representations[0].FilePath
	if filepath.Dir(target) != downloads {
		t.Fatalf("file is written to %s, want inside %s", target, downloads)
	}
	saved, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read the saved file: %v", err)
	}
	if !bytes.Equal(saved, data) {
		t.Fatalf("saved file holds %q", saved)
	}
}

// TestIncomingFileDoesNotOverwrite shows that a second file of the same name
// gets its own path.
func TestIncomingFileDoesNotOverwrite(t *testing.T) {
	downloads := filepath.Join(t.TempDir(), "downloads")
	f := newFixtureWith(t, func(opts *Options) {
		opts.DownloadsDir = downloads
		opts.Planner = fixedPlanner{outcome: Outcome{Action: ActionApply, Eager: true}}
	})

	data := []byte("one")
	rep := model.Representation{
		MIME:     MIMEUnisonFile,
		Size:     uint64(len(data)),
		Hash:     blake3.Sum256(data),
		Inline:   data,
		FileName: "note.txt",
	}
	first := textItem("", "")
	first.Kind = model.KindFiles
	first.Representations = []model.Representation{rep}
	if err := f.pipe.Apply(t.Context(), testPeer, first); err != nil {
		t.Fatalf("apply first item: %v", err)
	}
	firstPath, _ := f.board.LastWrite()

	second := textItem("", "")
	second.ID = model.ClipID{0x11}
	second.Kind = model.KindFiles
	second.Representations = []model.Representation{rep}
	if err := f.pipe.Apply(t.Context(), testPeer, second); err != nil {
		t.Fatalf("apply second item: %v", err)
	}
	secondContent, _ := f.board.LastWrite()

	if firstPath.Representations[0].FilePath == secondContent.Representations[0].FilePath {
		t.Fatal("the second file overwrote the first one")
	}
}

// TestInvalidItemIsRejected checks the announcement validation.
func TestInvalidItemIsRejected(t *testing.T) {
	f := newFixture(t)

	item := textItem("hello", "")
	item.Representations[0].Hash = [32]byte{}
	if err := f.pipe.Apply(t.Context(), testPeer, item); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("apply error is %v, want ErrUnsupportedContent", err)
	}

	item = textItem("hello", "")
	item.Representations[0].Inline = []byte("other")
	if err := f.pipe.Apply(t.Context(), testPeer, item); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("apply error is %v, want ErrUnsupportedContent", err)
	}
}

// representationData returns the payload of one representation.
func representationData(t *testing.T, store PayloadStore, rep model.Representation) []byte {
	t.Helper()

	if len(rep.Inline) > 0 {
		return rep.Inline
	}
	payload, err := store.Open(t.Context(), rep.Hash)
	if err != nil {
		t.Fatalf("open payload: %v", err)
	}
	defer func() { _ = payload.Close() }()

	data := make([]byte, payload.Size())
	if _, err := io.ReadFull(io.NewSectionReader(payload, 0, payload.Size()), data); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return data
}

// writeTempFile stores data in a file of the test directory.
func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}
