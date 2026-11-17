// Package journal writes the application log that the user can share.
//
// The log is off until the user turns it on in the settings. Every record of
// the core is written at debug level, and the window filters what it shows, so
// a shared copy always carries the full detail. Absolute paths of the user are
// replaced with a placeholder, like in the crash reports.
package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lebe-dev/unison/internal/redact"
)

// Names and limits of the application log.
const (
	// DirName holds the log files of the application.
	DirName = "logs"
	// FileName is the log the core writes.
	FileName = "journal.log"
	// MarkerName marks the journal as turned on. The file exists while the
	// user keeps the journal enabled.
	MarkerName = "journal.on"
	// MaxSize is the size a log file reaches before it rotates.
	MaxSize = 5 << 20
	// Backups is the number of rotated files kept next to the current one.
	Backups = 3
)

// timeFormat is the timestamp of every record, always in UTC.
const timeFormat = "2006-01-02T15:04:05.000Z"

// Store owns the journal file of a data directory and the state the settings
// show. A nil store accepts no records.
type Store struct {
	path   string
	marker string

	enabled atomic.Bool

	mu     sync.Mutex
	writer *Writer
	closed bool
}

// Path returns the journal file of a data directory.
func Path(dataDir string) string {
	return filepath.Join(dataDir, DirName, FileName)
}

// Marker returns the file that marks the journal as turned on.
func Marker(dataDir string) string {
	return filepath.Join(dataDir, DirName, MarkerName)
}

// AppFile returns the log file of the application shell in a data directory.
func AppFile(dataDir string) string {
	return filepath.Join(dataDir, DirName, "app.log")
}

// Open opens the journal of a data directory. The store is always returned, so
// a caller can keep logging through it; the error reports why the journal of a
// user who turned it on is not being written.
func Open(dataDir string) (*Store, error) {
	s := &Store{path: Path(dataDir), marker: Marker(dataDir)}
	if _, err := os.Stat(s.marker); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s, nil
		}
		return s, fmt.Errorf("journal: read the marker: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.openLocked(); err != nil {
		return s, err
	}
	return s, nil
}

// Enabled reports whether the user keeps the journal on.
func (s *Store) Enabled() bool {
	return s != nil && s.enabled.Load()
}

// SetEnabled turns the journal on or off and stores the choice. The records it
// writes while turning carry the change itself.
func (s *Store) SetEnabled(on bool) error {
	if s == nil {
		return errors.New("journal: no journal of this data directory")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("journal: the store is closed")
	}

	if on {
		if s.writer != nil {
			return nil
		}
		if err := s.openLocked(); err != nil {
			return err
		}
		if err := os.WriteFile(s.marker, []byte("on\n"), 0o600); err != nil {
			s.closeLocked()
			return fmt.Errorf("journal: store the marker: %w", err)
		}
		s.recordLocked(slog.LevelInfo, "application log enabled")
		return nil
	}

	if s.writer != nil {
		s.recordLocked(slog.LevelInfo, "application log disabled")
		s.closeLocked()
	}
	if err := os.Remove(s.marker); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("journal: remove the marker: %w", err)
	}
	return nil
}

// Handler returns the handler that writes the records of the core to the
// journal.
func (s *Store) Handler() slog.Handler {
	return &handler{store: s}
}

// Close closes the journal file. The store keeps the user choice.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.closeLocked()
	return nil
}

// openLocked opens the journal file and accepts records.
func (s *Store) openLocked() error {
	if s.writer != nil {
		return nil
	}
	writer, err := OpenWriter(s.path, MaxSize, Backups)
	if err != nil {
		return err
	}
	s.writer = writer
	s.enabled.Store(true)
	return nil
}

// closeLocked stops accepting records and closes the file.
func (s *Store) closeLocked() {
	s.enabled.Store(false)
	if s.writer != nil {
		_ = s.writer.Close()
		s.writer = nil
	}
}

// recordLocked writes one record of the journal itself.
func (s *Store) recordLocked(level slog.Level, message string) {
	if s.writer == nil {
		return
	}
	record := slog.NewRecord(time.Now(), level, message, 0)
	_, _ = s.writer.Write(format(record, nil, nil))
}

// append writes one record.
func (s *Store) append(record slog.Record, attrs []slog.Attr, groups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return nil
	}
	_, err := s.writer.Write(format(record, attrs, groups))
	return err
}

// handler writes the records of the core to the journal.
type handler struct {
	store  *Store
	attrs  []slog.Attr
	groups []string
}

// Enabled reports whether the journal takes records.
func (h *handler) Enabled(context.Context, slog.Level) bool {
	return h.store.Enabled()
}

// Handle writes one record.
func (h *handler) Handle(_ context.Context, record slog.Record) error {
	if !h.store.Enabled() {
		return nil
	}
	return h.store.append(record, h.attrs, h.groups)
}

// WithAttrs returns a handler that adds attrs to every record.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := &handler{store: h.store, groups: h.groups}
	next.attrs = append(next.attrs, h.attrs...)
	for _, attr := range attrs {
		next.attrs = appendAttr(next.attrs, prefix(h.groups), attr)
	}
	return next
}

// WithGroup returns a handler that puts the following attrs under name.
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := append(append([]string{}, h.groups...), name)
	return &handler{store: h.store, attrs: h.attrs, groups: groups}
}

// Tee returns a handler that writes every record to all handlers that accept
// its level.
func Tee(handlers ...slog.Handler) slog.Handler {
	switch len(handlers) {
	case 0:
		return slog.DiscardHandler
	case 1:
		return handlers[0]
	default:
		return &tee{handlers: handlers}
	}
}

// tee writes one record to several handlers.
type tee struct {
	handlers []slog.Handler
}

// Enabled reports whether any handler takes records.
func (t *tee) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range t.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle writes the record to every handler that accepts its level.
func (t *tee) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range t.handlers {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		if err := handler.Handle(ctx, record); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a tee of the handlers with the attrs.
func (t *tee) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(t.handlers))
	for i, handler := range t.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &tee{handlers: handlers}
}

// WithGroup returns a tee of the handlers with the group.
func (t *tee) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(t.handlers))
	for i, handler := range t.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &tee{handlers: handlers}
}

// format returns the line of a record: the time, the level, the message and
// the attributes. The line ends with a newline.
func format(record slog.Record, attrs []slog.Attr, groups []string) []byte {
	var b strings.Builder
	b.Grow(160)

	at := record.Time
	if at.IsZero() {
		at = time.Now()
	}
	b.WriteString(at.UTC().Format(timeFormat))
	b.WriteByte(' ')
	b.WriteString(levelName(record.Level))
	b.WriteByte(' ')
	b.WriteString(text(redact.Paths(record.Message)))

	for _, attr := range attrs {
		writeAttr(&b, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		writeAttr(&b, qualify(groups, attr))
		return true
	})
	b.WriteByte('\n')
	return []byte(b.String())
}

// writeAttr appends one attribute as key=value.
func writeAttr(b *strings.Builder, attr slog.Attr) {
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		for _, member := range value.Group() {
			writeAttr(b, qualify([]string{attr.Key}, member))
		}
		return
	}
	b.WriteByte(' ')
	b.WriteString(attr.Key)
	b.WriteByte('=')
	b.WriteString(quote(valueText(value)))
}

// appendAttr adds one attribute with the prefix of the groups to attrs and
// returns the result.
func appendAttr(attrs []slog.Attr, prefix string, attr slog.Attr) []slog.Attr {
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		for _, member := range value.Group() {
			attrs = appendAttr(attrs, prefix+attr.Key+".", member)
		}
		return attrs
	}
	return append(attrs, slog.Attr{Key: prefix + attr.Key, Value: value})
}

// qualify returns the attribute with its key under the groups.
func qualify(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 {
		return attr
	}
	return slog.Attr{Key: prefix(groups) + attr.Key, Value: attr.Value}
}

// prefix returns the key prefix of the groups.
func prefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return strings.Join(groups, ".") + "."
}

// valueText returns the text of a value. Errors keep their message and lose
// the paths of the user.
func valueText(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return redact.Paths(value.String())
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'g', -1, 64)
	case slog.KindBool:
		return strconv.FormatBool(value.Bool())
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindGroup:
		return ""
	case slog.KindAny:
		if err, ok := value.Any().(error); ok && err != nil {
			return redact.Paths(err.Error())
		}
		if value.Any() == nil {
			return "nil"
		}
		return redact.Paths(fmt.Sprint(value.Any()))
	default:
		return value.String()
	}
}

// text returns a message as it is, and escapes the characters that would break
// the line.
func text(message string) string {
	for _, r := range message {
		if r < ' ' || r == 0x7f {
			return strconv.Quote(message)
		}
	}
	return message
}

// quote wraps a text in double quotes when the line would be ambiguous
// without them.
func quote(text string) string {
	if text == "" {
		return `""`
	}
	for _, r := range text {
		if r <= ' ' || r == '"' || r == '=' || r == 0x7f {
			return strconv.Quote(text)
		}
	}
	return text
}

// levelName returns the level as a fixed token, so the filters of the window
// and the other platforms read the same names.
func levelName(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "DEBUG"
	case level < slog.LevelWarn:
		return "INFO"
	case level < slog.LevelError:
		return "WARN"
	default:
		return "ERROR"
	}
}
