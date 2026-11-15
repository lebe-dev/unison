package clip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// payload is one representation of a captured item before it is stored.
type payload struct {
	mime  string
	name  string
	mtime time.Time
	size  int64
	data  []byte
	path  string
}

// filePayloads describes the files of the clipboard. The files are read later,
// after the item gets its sequence number.
func (p *Pipeline) filePayloads(reps []ports.Representation) ([]payload, error) {
	payloads := make([]payload, 0, len(reps))
	for _, rep := range reps {
		item, err := filePayload(rep)
		if err != nil {
			if errors.Is(err, ErrUnsupportedContent) {
				p.log.Info("skipped clipboard file", "mime", rep.MIME)
				continue
			}
			return nil, err
		}
		p.log.Info("hashing clipboard file", "mime", MIMEUnisonFile, "size", item.size)
		payloads = append(payloads, item)
	}
	return payloads, nil
}

// filePayload describes one file of the clipboard.
func filePayload(rep ports.Representation) (payload, error) {
	info, err := os.Stat(rep.FilePath)
	if err != nil {
		return payload{}, fmt.Errorf("clip: read clipboard file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return payload{}, fmt.Errorf("%w: not a regular file", ErrUnsupportedContent)
	}
	return payload{
		mime:  MIMEUnisonFile,
		name:  filepath.Base(rep.FilePath),
		mtime: info.ModTime(),
		size:  info.Size(),
		path:  rep.FilePath,
	}, nil
}

// attachPayloads turns the payloads into representations: as many as fit stay
// inline, the rest goes to the payload store.
func (p *Pipeline) attachPayloads(ctx context.Context, payloads []payload) ([]model.Representation, error) {
	if len(payloads) > maxRepresentations {
		payloads = payloads[:maxRepresentations]
	}

	var inline int
	reps := make([]model.Representation, 0, len(payloads))
	for _, item := range payloads {
		rep, err := p.attachPayload(ctx, item, &inline)
		if err != nil {
			return nil, err
		}
		reps = append(reps, rep)
	}
	return reps, nil
}

// attachPayload builds one representation, inlining the payload while the item
// has room for it.
func (p *Pipeline) attachPayload(ctx context.Context, item payload, inline *int) (model.Representation, error) {
	if item.size < 0 {
		return model.Representation{}, fmt.Errorf("%w: negative payload size", ErrUnsupportedContent)
	}
	rep := model.Representation{
		MIME:     item.mime,
		Size:     uint64(item.size),
		FileName: item.name,
		MTime:    item.mtime,
	}

	if item.size <= int64(MaxInlineText-*inline) {
		data, err := readSource(item)
		if err != nil {
			return model.Representation{}, err
		}
		rep.Hash = hashOf(data)
		rep.Inline = data
		*inline += len(data)
		return rep, nil
	}

	hash, err := p.storePayload(ctx, item)
	if err != nil {
		return model.Representation{}, err
	}
	rep.Hash = hash
	return rep, nil
}

// readSource reads a small payload into memory.
func readSource(item payload) ([]byte, error) {
	if item.data != nil {
		return item.data, nil
	}

	file, err := os.Open(item.path)
	if err != nil {
		return nil, fmt.Errorf("clip: open payload: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("clip: stat payload: %w", err)
	}
	if info.Size() != item.size {
		return nil, fmt.Errorf("%w: payload changed while it was captured", ErrUnsupportedContent)
	}

	data := make([]byte, item.size)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, fmt.Errorf("clip: read payload: %w", err)
	}
	return data, nil
}

// storePayload puts one payload into the payload store.
func (p *Pipeline) storePayload(ctx context.Context, item payload) ([32]byte, error) {
	if p.payloads == nil {
		return [32]byte{}, fmt.Errorf("%w: payload of %d bytes", ErrUnsupportedContent, item.size)
	}

	source, closeSource, err := openSource(item)
	if err != nil {
		return [32]byte{}, err
	}
	defer closeSource()

	hash, err := p.payloads.Put(ctx, source, item.size)
	if err != nil {
		return [32]byte{}, fmt.Errorf("clip: store payload: %w", err)
	}
	if hash == ([32]byte{}) {
		return [32]byte{}, errors.New("clip: payload store returned an empty hash")
	}
	return hash, nil
}

// openSource returns a reader over the payload.
func openSource(item payload) (io.ReaderAt, func(), error) {
	if item.data != nil {
		return bytes.NewReader(item.data), func() {}, nil
	}

	file, err := os.Open(item.path)
	if err != nil {
		return nil, nil, fmt.Errorf("clip: open payload: %w", err)
	}
	return file, func() { _ = file.Close() }, nil
}

// payloadData reads one payload of an item into memory.
func (p *Pipeline) payloadData(ctx context.Context, rep model.Representation) ([]byte, error) {
	if len(rep.Inline) > 0 {
		return append([]byte(nil), rep.Inline...), nil
	}
	if p.payloads == nil {
		return nil, ErrPayloadMissing
	}
	if rep.Size > maxPayloadBytes {
		return nil, fmt.Errorf("%w: payload of %d bytes", ErrUnsupportedContent, rep.Size)
	}

	reader, err := p.payloads.Open(ctx, rep.Hash)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPayloadMissing, err)
	}
	defer func() { _ = reader.Close() }()

	if reader.Size() > maxPayloadBytes {
		return nil, fmt.Errorf("%w: payload of %d bytes", ErrUnsupportedContent, reader.Size())
	}
	data := make([]byte, reader.Size())
	if _, err := io.ReadFull(io.NewSectionReader(reader, 0, reader.Size()), data); err != nil {
		return nil, fmt.Errorf("clip: read payload: %w", err)
	}
	if hashOf(data) != rep.Hash {
		return nil, fmt.Errorf("%w: payload does not match its hash", ErrUnsupportedContent)
	}
	return data, nil
}

// hasPayload reports whether the payload of the representation is stored on
// this device.
func (p *Pipeline) hasPayload(ctx context.Context, rep model.Representation) (bool, error) {
	if len(rep.Inline) > 0 {
		return true, nil
	}
	if p.payloads == nil {
		return false, nil
	}
	return p.payloads.Has(ctx, rep.Hash)
}
