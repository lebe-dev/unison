package clip

import (
	"fmt"
	"strings"
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

// classify returns the kind of content and the payloads of the winning kind.
// Files take precedence over a single image, and an image over text: a
// clipboard often carries several views of the same selection.
func (p *Pipeline) classify(content ports.Content) (model.Kind, []payload, error) {
	texts, images, files := splitContent(content)

	switch {
	case len(files) > 0:
		payloads, err := p.filePayloads(files)
		return model.KindFiles, payloads, err
	case len(images) > 0:
		return model.KindImage, p.imagePayloads(images), nil
	default:
		return model.KindText, p.textPayloads(texts), nil
	}
}

// splitContent groups the representations of content by kind.
func splitContent(content ports.Content) (texts, images, files []ports.Representation) {
	for _, rep := range content.Representations {
		switch {
		case rep.FilePath != "":
			files = append(files, rep)
		case strings.HasPrefix(rep.MIME, "image/"):
			images = append(images, rep)
		case rep.MIME == MIMETextPlain || rep.MIME == MIMETextHTML:
			texts = append(texts, rep)
		}
	}
	return texts, images, files
}

// textPayloads returns the text payloads, plain text first. Payloads that are
// empty or not valid UTF-8 are left out.
func (p *Pipeline) textPayloads(reps []ports.Representation) []payload {
	byMIME := make(map[string][]byte, len(reps))
	for _, rep := range reps {
		if len(rep.Data) == 0 {
			continue
		}
		byMIME[rep.MIME] = rep.Data
	}

	payloads := make([]payload, 0, len(byMIME))
	for _, mime := range []string{MIMETextPlain, MIMETextHTML} {
		data, ok := byMIME[mime]
		if !ok {
			continue
		}
		if !utf8.Valid(data) {
			p.log.Debug("skipped text representation that is not valid utf-8", "mime", mime, "size", len(data))
			continue
		}
		payloads = append(payloads, payload{mime: mime, size: int64(len(data)), data: data})
	}
	return payloads
}

// hasFiles reports whether content carries a file payload.
func hasFiles(content ports.Content) bool {
	for _, rep := range content.Representations {
		if rep.FilePath != "" {
			return true
		}
	}
	return false
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

// hashOf returns the BLAKE3 hash of data.
func hashOf(data []byte) [32]byte {
	return blake3.Sum256(data)
}
