package journal

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testTime is the fixed time of the formatted records.
var testTime = time.Date(2026, 11, 17, 19, 48, 2, 123_000_000, time.UTC)

// TestFormat checks the line of one record: the time, the level, the message
// and the attributes.
func TestFormat(t *testing.T) {
	record := slog.NewRecord(testTime, slog.LevelDebug, "connected to the peer", 0)
	record.AddAttrs(slog.String("peer", "3f2a"), slog.Int("attempt", 2))
	record.AddAttrs(slog.Any("error", errors.New("open /Users/alice/file: denied")))
	record.AddAttrs(slog.Group("relay", slog.Bool("on", true), slog.String("note", "two words")))

	line := string(format(record, nil, nil))
	want := "2026-11-17T19:48:02.123Z DEBUG connected to the peer peer=3f2a attempt=2 " +
		`error="open [path]: denied" relay.on=true relay.note="two words"` + "\n"
	if line != want {
		t.Errorf("line is %q, want %q", line, want)
	}
}

// TestFormatKeepsTheLine checks that a message with a newline does not break the
// line of the record.
func TestFormatKeepsTheLine(t *testing.T) {
	record := slog.NewRecord(testTime, slog.LevelWarn, "two\nlines", 0)
	line := string(format(record, nil, nil))
	if strings.Count(line, "\n") != 1 || !strings.Contains(line, `"two\nlines"`) {
		t.Errorf("line is %q, want one line with an escaped message", line)
	}
}

// TestFormatWithGroups checks that the attributes added with a group keep it.
func TestFormatWithGroups(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetEnabled(true); err != nil {
		t.Fatalf("enable the journal: %v", err)
	}

	logger := slog.New(store.Handler()).With("before", 1).WithGroup("group").With("after", 2)
	logger.Info("a fact")

	content, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if want := "INFO a fact before=1 group.after=2"; !strings.Contains(string(content), want) {
		t.Errorf("the journal lost the groups: %q", content)
	}
}

// TestLevels checks the level tokens the filters of the window read.
func TestLevels(t *testing.T) {
	cases := map[slog.Level]string{
		slog.LevelDebug: "DEBUG",
		slog.LevelInfo:  "INFO",
		slog.LevelWarn:  "WARN",
		slog.LevelError: "ERROR",
	}
	for level, want := range cases {
		if got := levelName(level); got != want {
			t.Errorf("levelName(%v) = %q, want %q", level, got, want)
		}
	}
}

// TestJournalStaysOff checks that a journal nobody turned on writes nothing.
func TestJournalStaysOff(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if store.Enabled() {
		t.Fatal("a new journal is enabled")
	}
	slog.New(store.Handler()).Info("not written")

	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("a disabled journal wrote a file: %v", err)
	}
}

// TestJournalWritesTheRecords checks that the journal writes the debug records
// once the user turns it on and keeps the choice.
func TestJournalWritesTheRecords(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	log := slog.New(store.Handler())

	if err := store.SetEnabled(true); err != nil {
		t.Fatalf("enable the journal: %v", err)
	}
	log.Debug("a detail", "peer", "3f2a")

	content, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "INFO application log enabled") {
		t.Errorf("the journal does not report that it was enabled: %q", text)
	}
	if !strings.Contains(text, "DEBUG a detail peer=3f2a") {
		t.Errorf("the journal lost the debug record: %q", text)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close the journal: %v", err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatalf("open the journal again: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	if !again.Enabled() {
		t.Error("the journal forgot the choice of the user")
	}

	if err := again.SetEnabled(false); err != nil {
		t.Fatalf("disable the journal: %v", err)
	}
	if again.Enabled() {
		t.Error("a disabled journal is enabled")
	}
	if _, err := os.Stat(Marker(dir)); !os.IsNotExist(err) {
		t.Errorf("the marker was kept: %v", err)
	}
}

// TestJournalReplacesPaths checks that the records never carry the files of the
// user, in the message and in an error.
func TestJournalReplacesPaths(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetEnabled(true); err != nil {
		t.Fatalf("enable the journal: %v", err)
	}

	log := slog.New(store.Handler())
	log.Warn("cannot open /Users/alice/Library/Unison", "file", "/tmp/unison.log")
	log.Warn("read failed", "error", errors.New("open /home/alice/unison.db: denied"))

	content, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "alice") {
		t.Errorf("the journal carries a path of the user: %q", text)
	}
	if !strings.Contains(text, "WARN cannot open [path] file=[path]") {
		t.Errorf("the message lost its redaction: %q", text)
	}
	if !strings.Contains(text, `error="open [path]: denied"`) {
		t.Errorf("the error lost its redaction: %q", text)
	}
}

// TestTee checks that one record reaches every handler that accepts its level.
func TestTee(t *testing.T) {
	var debug, info strings.Builder
	logger := slog.New(Tee(
		slog.NewTextHandler(&debug, &slog.HandlerOptions{Level: slog.LevelDebug}),
		slog.NewTextHandler(&info, &slog.HandlerOptions{Level: slog.LevelInfo}),
	))

	logger.Debug("a detail")
	logger.Info("a fact")

	if !strings.Contains(debug.String(), "a detail") || !strings.Contains(debug.String(), "a fact") {
		t.Errorf("the debug handler lost a record: %q", debug.String())
	}
	if strings.Contains(info.String(), "a detail") {
		t.Errorf("the info handler took a debug record: %q", info.String())
	}
	if !strings.Contains(info.String(), "a fact") {
		t.Errorf("the info handler lost a record: %q", info.String())
	}
}

// TestWriterRotation checks that the log file rotates and keeps the backups it
// is configured for.
func TestWriterRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), DirName, FileName)
	writer, err := OpenWriter(path, 50, 2)
	if err != nil {
		t.Fatalf("open the log file: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	words := []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth"}
	for _, word := range words {
		if _, err := writer.Write([]byte(strings.Repeat(word+"\n", 3))); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	for _, name := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("stat %s: %v", filepath.Base(name), err)
		}
		if info.Size() > 50 {
			t.Errorf("%s is %d bytes, want at most 50", filepath.Base(name), info.Size())
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("the log keeps more than two backups: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log file: %v", err)
	}
	if !strings.Contains(string(content), "eighth") {
		t.Errorf("the current log file does not end with the last record: %q", content)
	}
}

// TestWriterSurvivesFailedRotation checks that a rotation that cannot move the
// files leaves the log writable, so the application does not go silent for the
// rest of its run.
func TestWriterSurvivesFailedRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	writer, err := OpenWriter(path, 10, 1)
	if err != nil {
		t.Fatalf("open the log file: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	// A directory with content in place of the backup cannot be removed, so
	// the rotation fails.
	if err := os.MkdirAll(filepath.Join(path+".1", "blocker"), 0o700); err != nil {
		t.Fatalf("create the blocker: %v", err)
	}

	for _, record := range []string{"0123456789", "after-failure\n", "still-written\n"} {
		if _, err := writer.Write([]byte(record)); err != nil {
			t.Fatalf("write %q: %v", record, err)
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log file: %v", err)
	}
	for _, want := range []string{"0123456789", "after-failure", "still-written"} {
		if !strings.Contains(string(content), want) {
			t.Errorf("the log file lost %q: %q", want, content)
		}
	}

	// Once the obstacle is gone the next write rotates again.
	if err := os.RemoveAll(path + ".1"); err != nil {
		t.Fatalf("remove the blocker: %v", err)
	}
	if _, err := writer.Write([]byte("rotated\n")); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read the backup: %v", err)
	}
	if !strings.Contains(string(backup), "still-written") {
		t.Errorf("the backup does not hold the earlier records: %q", backup)
	}
}
