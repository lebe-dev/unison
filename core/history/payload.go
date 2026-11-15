package history

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// MIME types the history knows by name.
const (
	mimeTextPlain = "text/plain"
	mimeTextHTML  = "text/html"
)

// Fetch restores the payloads of one item from the peers. When path is not
// empty the payloads are written there as well: a single payload goes to path,
// several payloads go into the directory path.
func (s *Store) Fetch(ctx context.Context, id model.ClipID, path string) (Item, []string, error) {
	item, err := s.Get(ctx, id)
	if err != nil {
		return Item{}, nil, err
	}
	if err := s.download(ctx, item.ClipItem); err != nil {
		return Item{}, nil, err
	}
	if item, err = s.Get(ctx, id); err != nil {
		return Item{}, nil, err
	}
	if path == "" {
		return item, nil, nil
	}

	written, err := s.writePayloads(ctx, item.ClipItem, path)
	if err != nil {
		return Item{}, written, err
	}
	return item, written, nil
}

// Copy writes one item back to the local clipboard.
func (s *Store) Copy(ctx context.Context, id model.ClipID) error {
	if s.clip == nil {
		return fmt.Errorf("%w: no clipboard", ErrUnavailable)
	}
	item, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	content, err := s.content(ctx, item.ClipItem)
	if err != nil {
		return err
	}
	if err := s.clip.Write(ctx, content); err != nil {
		return fmt.Errorf("history: write clipboard: %w", err)
	}
	s.log.Info("history item copied", "id", id.String(), "kind", item.Kind.String())
	return nil
}

// download restores the payloads of the item the device is missing.
func (s *Store) download(ctx context.Context, item model.ClipItem) error {
	for _, rep := range item.Representations {
		if len(rep.Inline) > 0 || rep.Hash == [32]byte{} {
			continue
		}
		present, err := s.present(ctx, rep.Hash)
		if err != nil {
			return err
		}
		if present {
			continue
		}
		if s.fetch == nil {
			return fmt.Errorf("%w: cannot download a payload", ErrUnavailable)
		}
		if err := s.fetch(ctx, rep.Hash); err != nil {
			return err
		}
		if err := s.MarkPresent(ctx, rep.Hash, true); err != nil {
			return err
		}
	}
	return nil
}

// content turns an item into clipboard content.
func (s *Store) content(ctx context.Context, item model.ClipItem) (ports.Content, error) {
	switch item.Kind {
	case model.KindText:
		return s.textContent(item)
	case model.KindImage:
		rep, ok := primaryPayload(item)
		if !ok {
			return ports.Content{}, fmt.Errorf("%w: no image payload", ErrUnsupported)
		}
		data, err := s.readPayload(ctx, rep)
		if err != nil {
			return ports.Content{}, err
		}
		return ports.Content{Representations: []ports.Representation{{MIME: rep.MIME, Data: data}}}, nil
	default:
		return ports.Content{}, fmt.Errorf("%w: kind %s", ErrUnsupported, item.Kind)
	}
}

// textContent returns the inline text representations of the item.
func (s *Store) textContent(item model.ClipItem) (ports.Content, error) {
	var reps []ports.Representation
	var size int
	for _, rep := range item.Representations {
		if rep.MIME != mimeTextPlain && rep.MIME != mimeTextHTML {
			continue
		}
		if len(rep.Inline) == 0 {
			return ports.Content{}, fmt.Errorf("%w: %s has no inline payload", ErrUnavailable, rep.MIME)
		}
		size += len(rep.Inline)
		reps = append(reps, ports.Representation{MIME: rep.MIME, Data: append([]byte(nil), rep.Inline...)})
	}
	if len(reps) == 0 {
		return ports.Content{}, fmt.Errorf("%w: no text representation", ErrUnsupported)
	}
	if size > maxInlineBytes {
		return ports.Content{}, fmt.Errorf("%w: inline payload is %d bytes", ErrUnsupported, size)
	}
	return ports.Content{Representations: reps}, nil
}

// writePayloads writes the payloads of the item under path.
func (s *Store) writePayloads(ctx context.Context, item model.ClipItem, path string) ([]string, error) {
	targets := writeTargets(item)
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: no payload to write", ErrUnsupported)
	}

	directory := len(targets) > 1
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		directory = true
	}
	if directory {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, fmt.Errorf("history: create directory: %w", err)
		}
	}

	written := make([]string, 0, len(targets))
	for i, rep := range targets {
		name := path
		if directory {
			name = filepath.Join(path, payloadName(rep, i))
		}
		if err := s.writePayload(ctx, rep, name); err != nil {
			return written, err
		}
		written = append(written, name)
	}
	return written, nil
}

// writePayload writes one payload to name.
func (s *Store) writePayload(ctx context.Context, rep model.Representation, name string) error {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("history: create payload file: %w", err)
	}

	written, err := s.copyPayload(ctx, rep, file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if err := closeErr; err != nil {
		return fmt.Errorf("history: close payload file: %w", err)
	}
	if uint64(written) != rep.Size && len(rep.Inline) == 0 {
		return fmt.Errorf("history: wrote %d bytes of a %d byte payload", written, rep.Size)
	}
	return nil
}

// copyPayload copies one payload into w.
func (s *Store) copyPayload(ctx context.Context, rep model.Representation, w io.Writer) (int64, error) {
	if len(rep.Inline) > 0 {
		written, err := w.Write(rep.Inline)
		return int64(written), err
	}
	if s.payloads == nil {
		return 0, fmt.Errorf("%w: no payload store", ErrUnavailable)
	}

	payload, err := s.payloads.Open(ctx, rep.Hash)
	if err != nil {
		return 0, fmt.Errorf("history: open payload: %w", err)
	}
	defer func() { _ = payload.Close() }()

	written, err := io.Copy(w, io.NewSectionReader(payload, 0, payload.Size()))
	if err != nil {
		return written, fmt.Errorf("history: read payload: %w", err)
	}
	return written, nil
}

// readPayload reads one payload into memory.
func (s *Store) readPayload(ctx context.Context, rep model.Representation) ([]byte, error) {
	if rep.Size > maxPayloadOpenBytes {
		return nil, fmt.Errorf("%w: payload is %d bytes", ErrUnsupported, rep.Size)
	}
	var buffer bytes.Buffer
	if _, err := s.copyPayload(ctx, rep, &buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// writeTargets returns the representations Fetch writes: the plain text of a
// text item, the primary image of an image item and every file of a file item.
func writeTargets(item model.ClipItem) []model.Representation {
	switch item.Kind {
	case model.KindText:
		for _, rep := range item.Representations {
			if rep.MIME == mimeTextPlain {
				return []model.Representation{rep}
			}
		}
		return nil
	case model.KindImage:
		rep, ok := primaryPayload(item)
		if !ok {
			return nil
		}
		return []model.Representation{rep}
	case model.KindFiles:
		files := make([]model.Representation, 0, len(item.Representations))
		for _, rep := range item.Representations {
			if rep.FileName != "" {
				files = append(files, rep)
			}
		}
		return files
	default:
		return nil
	}
}

// primaryPayload returns the first representation with a payload.
func primaryPayload(item model.ClipItem) (model.Representation, bool) {
	for _, rep := range item.Representations {
		if len(rep.Inline) > 0 || rep.Hash != [32]byte{} {
			return rep, true
		}
	}
	return model.Representation{}, false
}

// payloadName returns the file name of one payload.
func payloadName(rep model.Representation, index int) string {
	if name := filepath.Base(rep.FileName); rep.FileName != "" && name != "." && name != "/" {
		return name
	}
	extension := ".bin"
	if types, err := mime.ExtensionsByType(rep.MIME); err == nil && len(types) > 0 {
		extension = types[0]
	}
	if index == 0 {
		return "clip" + extension
	}
	return fmt.Sprintf("clip-%d%s", index+1, extension)
}
