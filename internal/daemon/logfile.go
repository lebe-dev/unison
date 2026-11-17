package daemon

import (
	"io"
	"log/slog"

	"github.com/lebe-dev/unison/internal/journal"
)

// Log file limits.
const (
	// LogMaxSize is the size a log file reaches before it rotates.
	LogMaxSize = 5 << 20
	// LogBackups is the number of rotated files kept next to the current one.
	LogBackups = 3
)

// openLog opens the log file of the data directory and returns its logger.
// stderr copies the records to the given writer, which is how a daemon started
// from a terminal shows its log. The application log takes the debug records
// when the user turned it on.
func openLog(dataDir string, level slog.Level, stderr io.Writer, appLog *journal.Store) (*slog.Logger, func(), error) {
	file, err := journal.OpenWriter(LogFile(dataDir), LogMaxSize, LogBackups)
	if err != nil {
		return nil, nil, err
	}

	var out io.Writer = file
	if stderr != nil {
		out = io.MultiWriter(file, stderr)
	}
	handlers := []slog.Handler{slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})}
	if appLog != nil {
		handlers = append(handlers, appLog.Handler())
	}
	return slog.New(journal.Tee(handlers...)), func() { _ = file.Close() }, nil
}
