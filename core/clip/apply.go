package clip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// Download limits.
const (
	// downloadsDirName is the directory of received files inside the user home.
	downloadsDirName = "Unison"
	// maxFileNameLength bounds a file name taken from an item.
	maxFileNameLength = 200
	// maxDownloadAttempts bounds the lookups of a free file name.
	maxDownloadAttempts = 100
)

// DefaultDownloadsDir returns the directory that receives incoming files.
func DefaultDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads", downloadsDirName)
}

// Apply handles an item announced by a peer. The item reaches the history and,
// when the planner allows it, the local clipboard.
func (p *Pipeline) Apply(ctx context.Context, from model.DeviceID, item model.ClipItem) error {
	if from.IsZero() {
		return errors.New("clip: peer device is required")
	}

	paused, err := p.pausedAt(ctx)
	if err != nil {
		return err
	}
	if paused {
		p.log.Debug("dropped incoming item while paused", "id", item.ID.String(), "origin", item.Origin.String())
		return nil
	}
	if item.Deleted {
		return nil
	}
	if err := validateItem(item); err != nil {
		return err
	}

	outcome, err := p.planner.Plan(ctx, from, item)
	if err != nil {
		return fmt.Errorf("clip: decide policy: %w", err)
	}
	if outcome.Action == ActionSkip {
		p.log.Debug("incoming item skipped", "id", item.ID.String(), "kind", item.Kind.String())
		return nil
	}

	if err := p.history.Append(ctx, item); err != nil {
		return fmt.Errorf("clip: record item: %w", err)
	}

	switch outcome.Action {
	case ActionApply:
		return p.applyItem(ctx, from, item, outcome.Eager)
	case ActionConfirm:
		p.report(ctx, Event{Kind: EventApply, Peer: from, Item: item})
	case ActionMetadata:
		p.report(ctx, Event{Kind: EventDownload, Peer: from, Item: item})
	}
	return nil
}

// applyItem fetches the missing payloads and writes the item to the clipboard.
// An item whose payload cannot be fetched stays in the history and is reported
// for download.
func (p *Pipeline) applyItem(ctx context.Context, from model.DeviceID, item model.ClipItem, eager bool) error {
	missing, err := p.missingPayloads(ctx, item)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		if !eager {
			p.report(ctx, Event{Kind: EventDownload, Peer: from, Item: item})
			return nil
		}
		if err := p.fetchPayloads(ctx, missing); err != nil {
			p.log.Warn("fetching the payloads of an item failed", "id", item.ID.String(), "error", err)
			p.report(ctx, Event{Kind: EventDownload, Peer: from, Item: item})
			return nil
		}
	}

	content, err := p.clipboardContent(ctx, item)
	if errors.Is(err, ErrPayloadMissing) {
		p.log.Warn("the payload of an item is not available", "id", item.ID.String(), "kind", item.Kind.String())
		p.report(ctx, Event{Kind: EventDownload, Peer: from, Item: item})
		return nil
	}
	if err != nil {
		return err
	}
	return p.writeItem(ctx, from, item, content)
}

// ApplyHistory writes a recorded item to the local clipboard. The user asked
// for the item, so the policy and the history are not involved. A payload that
// is missing locally is downloaded first.
func (p *Pipeline) ApplyHistory(ctx context.Context, item model.ClipItem) error {
	if err := validateItem(item); err != nil {
		return err
	}
	missing, err := p.missingPayloads(ctx, item)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		if err := p.fetchPayloads(ctx, missing); err != nil {
			return fmt.Errorf("clip: fetch the payload of the item: %w", err)
		}
	}
	content, err := p.clipboardContent(ctx, item)
	if err != nil {
		return err
	}
	return p.writeItem(ctx, item.Origin, item, content)
}

// writeItem writes the content of an item to the clipboard.
func (p *Pipeline) writeItem(ctx context.Context, from model.DeviceID, item model.ClipItem, content ports.Content) error {
	// The hashes are registered before the write so a watcher that reports the
	// change from inside Write already sees it as own content.
	p.echo.register(content)
	if err := p.clipboard.Write(ctx, content); err != nil {
		return fmt.Errorf("clip: write clipboard: %w", err)
	}
	p.log.Info("applied clip item", "id", item.ID.String(), "kind", item.Kind.String())
	p.report(ctx, Event{Kind: EventApplied, Peer: from, Item: item})
	return nil
}

// missingPayloads returns the payloads of the item that are not stored locally.
func (p *Pipeline) missingPayloads(ctx context.Context, item model.ClipItem) ([][32]byte, error) {
	var missing [][32]byte
	for _, rep := range item.Representations {
		present, err := p.hasPayload(ctx, rep)
		if err != nil {
			return nil, fmt.Errorf("clip: read payload state: %w", err)
		}
		if !present && !containsHash(missing, rep.Hash) {
			missing = append(missing, rep.Hash)
		}
	}
	return missing, nil
}

// fetchPayloads downloads the payloads from the peers.
func (p *Pipeline) fetchPayloads(ctx context.Context, hashes [][32]byte) error {
	if p.fetch == nil {
		return fmt.Errorf("%w: no fetcher", ErrPayloadMissing)
	}
	for _, hash := range hashes {
		if err := p.fetch.Fetch(ctx, hash); err != nil {
			return err
		}
		p.markPresent(ctx, hash)
	}
	return nil
}

// markPresent keeps the payload state of the history in step with a download.
func (p *Pipeline) markPresent(ctx context.Context, hash [32]byte) {
	sink, ok := p.history.(PresenceSink)
	if !ok {
		return
	}
	if err := sink.MarkPresent(ctx, hash, true); err != nil {
		p.log.Warn("recording the payload state failed", "error", err)
	}
}

// clipboardContent turns an item into the content the platform writes.
func (p *Pipeline) clipboardContent(ctx context.Context, item model.ClipItem) (ports.Content, error) {
	switch item.Kind {
	case model.KindText:
		return p.textContent(ctx, item)
	case model.KindImage:
		return p.imageContent(ctx, item)
	case model.KindFiles:
		return p.fileContent(ctx, item)
	default:
		return ports.Content{}, fmt.Errorf("%w: kind %s", ErrUnsupportedContent, item.Kind)
	}
}

// textContent returns the text representations of the item.
func (p *Pipeline) textContent(ctx context.Context, item model.ClipItem) (ports.Content, error) {
	reps := make([]ports.Representation, 0, len(item.Representations))
	for _, rep := range item.Representations {
		if rep.MIME != MIMETextPlain && rep.MIME != MIMETextHTML {
			continue
		}
		data, err := p.payloadData(ctx, rep)
		if err != nil {
			return ports.Content{}, err
		}
		reps = append(reps, ports.Representation{MIME: rep.MIME, Data: data})
	}
	if len(reps) == 0 {
		return ports.Content{}, fmt.Errorf("%w: no text representation", ErrUnsupportedContent)
	}
	return ports.Content{Representations: reps}, nil
}

// imageContent returns the PNG representation of the item.
func (p *Pipeline) imageContent(ctx context.Context, item model.ClipItem) (ports.Content, error) {
	rep, ok := primaryImage(item)
	if !ok {
		return ports.Content{}, fmt.Errorf("%w: no image representation", ErrUnsupportedContent)
	}
	data, err := p.payloadData(ctx, rep)
	if err != nil {
		return ports.Content{}, err
	}
	data, err = toPNG(data)
	if err != nil {
		return ports.Content{}, fmt.Errorf("%w: %w", ErrUnsupportedContent, err)
	}
	return ports.Content{Representations: []ports.Representation{{MIME: MIMEImagePNG, Data: data}}}, nil
}

// fileContent writes the files of the item to the downloads directory and
// returns their paths.
func (p *Pipeline) fileContent(ctx context.Context, item model.ClipItem) (ports.Content, error) {
	if p.downloads == "" {
		return ports.Content{}, fmt.Errorf("%w: no downloads directory", ErrUnsupportedContent)
	}
	if err := os.MkdirAll(p.downloads, 0o700); err != nil {
		return ports.Content{}, fmt.Errorf("clip: create downloads directory: %w", err)
	}

	var reps []ports.Representation
	for i, rep := range item.Representations {
		if rep.FileName == "" {
			continue
		}
		target := p.downloadPath(rep, i)
		if err := p.writeFile(ctx, rep, target); err != nil {
			return ports.Content{}, err
		}
		p.log.Info("saved incoming file", "mime", rep.MIME, "size", rep.Size)
		reps = append(reps, ports.Representation{MIME: MIMEUnisonFile, FilePath: target})
	}
	if len(reps) == 0 {
		return ports.Content{}, fmt.Errorf("%w: no file representation", ErrUnsupportedContent)
	}
	return ports.Content{Representations: reps}, nil
}

// writeFile copies one payload into a new file.
func (p *Pipeline) writeFile(ctx context.Context, rep model.Representation, target string) error {
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("clip: create file: %w", err)
	}

	written, err := p.copyPayload(ctx, rep, file)
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(target)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("clip: close file: %w", closeErr)
	}
	// The announced size is only checked for an inline payload, which is
	// already verified against its hash. A stored payload carries its own size.
	if len(rep.Inline) > 0 && written != int64(len(rep.Inline)) {
		_ = os.Remove(target)
		return fmt.Errorf("clip: wrote %d bytes of a %d byte file", written, len(rep.Inline))
	}
	return nil
}

// copyPayload copies one payload into w.
func (p *Pipeline) copyPayload(ctx context.Context, rep model.Representation, w io.Writer) (int64, error) {
	if len(rep.Inline) > 0 {
		written, err := w.Write(rep.Inline)
		return int64(written), err
	}
	if p.payloads == nil {
		return 0, ErrPayloadMissing
	}

	payload, err := p.payloads.Open(ctx, rep.Hash)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrPayloadMissing, err)
	}
	defer func() { _ = payload.Close() }()

	written, err := io.Copy(w, io.NewSectionReader(payload, 0, payload.Size()))
	if err != nil {
		return written, fmt.Errorf("clip: read payload: %w", err)
	}
	return written, nil
}

// downloadPath returns a free file name in the downloads directory.
func (p *Pipeline) downloadPath(rep model.Representation, index int) string {
	name := downloadName(rep, index)
	for attempt := range maxDownloadAttempts {
		candidate := filepath.Join(p.downloads, numberedName(name, attempt))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return filepath.Join(p.downloads, numberedName(name, maxDownloadAttempts))
}

// downloadName returns a safe file name for one payload.
func downloadName(rep model.Representation, index int) string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '-'
		}
		return r
	}, rep.FileName)
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == ".." {
		return fmt.Sprintf("file-%d.bin", index+1)
	}
	if len(name) > maxFileNameLength {
		name = strings.ToValidUTF8(name[:maxFileNameLength], "")
	}
	return name
}

// numberedName adds a counter to a file name that is already taken.
func numberedName(name string, attempt int) string {
	if attempt == 0 {
		return name
	}
	extension := filepath.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, extension), attempt, extension)
}

// primaryImage returns the PNG representation of the item, or its first image.
func primaryImage(item model.ClipItem) (model.Representation, bool) {
	var fallback model.Representation
	var found bool
	for _, rep := range item.Representations {
		if rep.MIME == MIMEImagePNG {
			return rep, true
		}
		if !found && strings.HasPrefix(rep.MIME, "image/") {
			fallback, found = rep, true
		}
	}
	return fallback, found
}

// containsHash reports whether hashes hold hash.
func containsHash(hashes [][32]byte, hash [32]byte) bool {
	for _, known := range hashes {
		if known == hash {
			return true
		}
	}
	return false
}

// validateItem checks the parts of an announced item the pipeline relies on.
// Inline payloads are verified before they are trusted.
func validateItem(item model.ClipItem) error {
	switch {
	case item.ID.IsZero():
		return fmt.Errorf("%w: id is zero", ErrUnsupportedContent)
	case item.Origin.IsZero():
		return fmt.Errorf("%w: origin is zero", ErrUnsupportedContent)
	case item.Seq == 0:
		return fmt.Errorf("%w: sequence is zero", ErrUnsupportedContent)
	case item.Kind < model.KindText || item.Kind > model.KindFiles:
		return fmt.Errorf("%w: kind %d", ErrUnsupportedContent, item.Kind)
	case len(item.Representations) > maxRepresentations:
		return fmt.Errorf("%w: %d representations", ErrUnsupportedContent, len(item.Representations))
	}

	var inline int
	for i, rep := range item.Representations {
		switch {
		case rep.MIME == "":
			return fmt.Errorf("%w: representation %d has no mime type", ErrUnsupportedContent, i)
		case rep.Hash == ([32]byte{}):
			return fmt.Errorf("%w: representation %d has no hash", ErrUnsupportedContent, i)
		case rep.Size > maxPayloadBytes && rep.MIME != MIMEUnisonFile:
			return fmt.Errorf("%w: representation %d has %d bytes", ErrUnsupportedContent, i, rep.Size)
		}
		if len(rep.Inline) == 0 {
			continue
		}
		if uint64(len(rep.Inline)) != rep.Size {
			return fmt.Errorf("%w: representation %d carries %d bytes of %d", ErrUnsupportedContent, i, len(rep.Inline), rep.Size)
		}
		if blake3.Sum256(rep.Inline) != rep.Hash {
			return fmt.Errorf("%w: representation %d does not match its hash", ErrUnsupportedContent, i)
		}
		inline += len(rep.Inline)
	}
	if inline > MaxInlineText {
		return fmt.Errorf("%w: %d bytes inline, limit is %d", ErrUnsupportedContent, inline, MaxInlineText)
	}
	return nil
}
