package clip

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"
	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// newID returns a new time ordered clip identifier.
func newID() (model.ClipID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.ClipID{}, fmt.Errorf("clip: create id: %w", err)
	}
	return model.ClipID(id), nil
}

// textRepresentations extracts the text payload of clipboard content, plain
// text first. Representations that are missing, empty, not text or not valid
// UTF-8 are left out.
func (p *Pipeline) textRepresentations(content ports.Content) []model.Representation {
	payload := make(map[string][]byte, 2)
	for _, rep := range content.Representations {
		if rep.MIME != MIMETextPlain && rep.MIME != MIMETextHTML {
			continue
		}
		if len(rep.Data) == 0 || rep.FilePath != "" {
			continue
		}
		payload[rep.MIME] = rep.Data
	}

	reps := make([]model.Representation, 0, len(payload))
	for _, mime := range []string{MIMETextPlain, MIMETextHTML} {
		data, ok := payload[mime]
		if !ok {
			continue
		}
		if !utf8.Valid(data) {
			p.log.Debug("skipped text representation that is not valid utf-8", "mime", mime, "size", len(data))
			continue
		}
		reps = append(reps, model.Representation{
			MIME:   mime,
			Size:   uint64(len(data)),
			Hash:   blake3.Sum256(data),
			Inline: data,
		})
	}
	return reps
}

// inlineSize returns the payload size of reps in bytes.
func inlineSize(reps []model.Representation) int {
	var size int
	for _, rep := range reps {
		size += len(rep.Inline)
	}
	return size
}

// itemContent turns the inline text representations of item into clipboard
// content. Items without inline text are rejected.
func itemContent(item model.ClipItem) (ports.Content, error) {
	if item.Kind != model.KindText {
		return ports.Content{}, fmt.Errorf("%w: kind %s", ErrUnsupportedContent, item.Kind)
	}

	reps := make([]ports.Representation, 0, len(item.Representations))
	var size int
	for _, rep := range item.Representations {
		if rep.MIME != MIMETextPlain && rep.MIME != MIMETextHTML {
			continue
		}
		if len(rep.Inline) == 0 {
			return ports.Content{}, fmt.Errorf("%w: %s has no inline payload", ErrUnsupportedContent, rep.MIME)
		}
		size += len(rep.Inline)
		reps = append(reps, ports.Representation{MIME: rep.MIME, Data: slices.Clone(rep.Inline)})
	}
	if len(reps) == 0 {
		return ports.Content{}, fmt.Errorf("%w: no text representation", ErrUnsupportedContent)
	}
	if size > MaxInlineText {
		return ports.Content{}, fmt.Errorf("%w: %d bytes above the inline limit", ErrUnsupportedContent, size)
	}
	return ports.Content{Representations: reps}, nil
}

// mergeContent adds the representations of next to pending, replacing the ones
// with the same MIME type.
func mergeContent(pending, next ports.Content) ports.Content {
	merged := make([]ports.Representation, 0, len(pending.Representations)+len(next.Representations))
	for _, rep := range pending.Representations {
		if !hasMIME(next.Representations, rep.MIME) {
			merged = append(merged, rep)
		}
	}
	merged = append(merged, next.Representations...)
	return ports.Content{Representations: merged}
}

// hasMIME reports whether reps hold a representation of the given type.
func hasMIME(reps []ports.Representation, mime string) bool {
	for _, rep := range reps {
		if rep.MIME == mime {
			return true
		}
	}
	return false
}
