package share

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lebe-dev/unison/core/session"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// newID returns a random transfer ID.
func newID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("share: transfer id: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

// checkTransferID validates an ID taken from the network.
func checkTransferID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("%w: empty transfer id", ErrInvalidFile)
	case len(id) > session.MaxTransferIDLength:
		return fmt.Errorf("%w: transfer id of %d bytes", ErrInvalidFile, len(id))
	case !utf8.ValidString(id):
		return fmt.Errorf("%w: transfer id is not valid UTF-8", ErrInvalidFile)
	}
	for _, r := range id {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("%w: transfer id of %q", ErrInvalidFile, id)
		}
	}
	return nil
}

// offerMessage returns the control message that offers the transfer.
func offerMessage(transfer Transfer) *wirev1.ControlMessage {
	files := make([]*wirev1.FileMeta, 0, len(transfer.Files))
	for _, file := range transfer.Files {
		files = append(files, &wirev1.FileMeta{
			Name:        file.Name,
			Size:        uint64(max(file.Size, 0)),
			Blake3:      file.Hash[:],
			MtimeUnixMs: unixMilli(file.MTime),
			MimeType:    file.MIME,
		})
	}
	return &wirev1.ControlMessage{Body: &wirev1.ControlMessage_ShareOffer{
		ShareOffer: &wirev1.ShareOffer{TransferId: transfer.ID, Files: files},
	}}
}

// decisionMessage returns the control message that answers an offer.
func decisionMessage(id string, accepted bool) *wirev1.ControlMessage {
	return &wirev1.ControlMessage{Body: &wirev1.ControlMessage_ShareDecision{
		ShareDecision: &wirev1.ShareDecision{TransferId: id, Accepted: accepted},
	}}
}

// cancelMessage returns the control message that stops a transfer.
func cancelMessage(id string) *wirev1.ControlMessage {
	return &wirev1.ControlMessage{Body: &wirev1.ControlMessage_ShareCancel{
		ShareCancel: &wirev1.ShareCancel{TransferId: id},
	}}
}

// resultMessage returns the control message that reports the outcome.
func resultMessage(id string, ok bool, reason string) *wirev1.ControlMessage {
	return &wirev1.ControlMessage{Body: &wirev1.ControlMessage_ShareResult{
		ShareResult: &wirev1.ShareResult{TransferId: id, Ok: ok, Error: reason},
	}}
}

// filesOf validates the files of an offer and returns them.
func filesOf(metas []*wirev1.FileMeta) ([]File, error) {
	if len(metas) == 0 {
		return nil, ErrNoFiles
	}
	if len(metas) > MaxFiles {
		return nil, ErrTooManyFiles
	}

	files := make([]File, 0, len(metas))
	for _, meta := range metas {
		if meta == nil {
			return nil, fmt.Errorf("%w: empty file", ErrInvalidFile)
		}
		if err := checkName(meta.GetName()); err != nil {
			return nil, err
		}
		if meta.GetSize() > 1<<62 {
			return nil, fmt.Errorf("%w: size %d", ErrInvalidFile, meta.GetSize())
		}
		if len(meta.GetBlake3()) != 32 {
			return nil, fmt.Errorf("%w: hash of %d bytes", ErrInvalidFile, len(meta.GetBlake3()))
		}

		var hash [32]byte
		copy(hash[:], meta.GetBlake3())
		if hash == ([32]byte{}) {
			return nil, fmt.Errorf("%w: zero hash", ErrInvalidFile)
		}
		files = append(files, File{
			Name:  SafeName(meta.GetName()),
			Size:  int64(meta.GetSize()),
			Hash:  hash,
			MTime: timeFromUnixMilli(meta.GetMtimeUnixMs()),
			MIME:  meta.GetMimeType(),
		})
	}
	return files, nil
}

// mimeOf returns the content type guessed from a file name.
func mimeOf(name string) string {
	if extension := filepath.Ext(name); extension != "" {
		return mime.TypeByExtension(strings.ToLower(extension))
	}
	return ""
}

// unixMilli converts t to unix milliseconds. The zero time maps to 0.
func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// timeFromUnixMilli converts unix milliseconds to a time. Zero maps to the zero
// time.
func timeFromUnixMilli(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// checkFile validates a file before it is offered.
func checkFile(file File) error {
	if err := checkName(file.Name); err != nil {
		return err
	}
	switch {
	case file.Hash == ([32]byte{}):
		return fmt.Errorf("%w: zero hash", ErrInvalidFile)
	case file.Size < 0:
		return fmt.Errorf("%w: size %d", ErrInvalidFile, file.Size)
	}
	return nil
}
