package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogFileRotation checks that the log file rotates and keeps the backups
// it is configured for.
func TestLogFileRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), LogsDirName, LogFileName)
	file, err := openLogFile(path, 50, 2)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	words := []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth"}
	for _, word := range words {
		if _, err := file.Write([]byte(strings.Repeat(word+"\n", 3))); err != nil {
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

// TestLogRecords checks that the log of a data directory carries JSON records
// and goes to the data directory.
func TestLogRecords(t *testing.T) {
	dataDir := t.TempDir()
	logger, closeLog, err := openLog(dataDir, slog.LevelInfo, nil)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	logger.Info("daemon started", "version", "test")
	logger.Debug("not logged")

	content, err := os.ReadFile(LogFile(dataDir))
	if err != nil {
		closeLog()
		t.Fatalf("read the log file: %v", err)
	}
	closeLog()

	var record map[string]any
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatalf("log record is not JSON: %v", err)
	}
	if record["msg"] != "daemon started" {
		t.Errorf("message is %v, want %q", record["msg"], "daemon started")
	}
	if record["version"] != "test" {
		t.Errorf("version is %v, want %q", record["version"], "test")
	}
}
