package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/internal/journal"
)

// TestLogRecords checks that the log of a data directory carries JSON records
// and goes to the data directory.
func TestLogRecords(t *testing.T) {
	dataDir := t.TempDir()
	logger, closeLog, err := openLog(dataDir, slog.LevelInfo, nil, nil)
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

// TestLogWritesTheJournal checks that the logger writes the debug records to
// the application log when the user turned it on.
func TestLogWritesTheJournal(t *testing.T) {
	dataDir := t.TempDir()
	appLog, err := journal.Open(dataDir)
	if err != nil {
		t.Fatalf("open the journal: %v", err)
	}
	t.Cleanup(func() { _ = appLog.Close() })
	if err := appLog.SetEnabled(true); err != nil {
		t.Fatalf("enable the journal: %v", err)
	}

	logger, closeLog, err := openLog(dataDir, slog.LevelInfo, nil, appLog)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	logger.Debug("the daemon detail", "peer", "3f2a")
	closeLog()

	content, err := os.ReadFile(journal.Path(dataDir))
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if want := "DEBUG the daemon detail peer=3f2a"; !strings.Contains(string(content), want) {
		t.Errorf("the journal lost the record %q: %q", want, content)
	}
}
