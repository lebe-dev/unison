package daemon

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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
// from a terminal shows its log.
func openLog(dataDir string, level slog.Level, stderr io.Writer) (*slog.Logger, func(), error) {
	file, err := openLogFile(LogFile(dataDir), LogMaxSize, LogBackups)
	if err != nil {
		return nil, nil, err
	}

	var out io.Writer = file
	if stderr != nil {
		out = io.MultiWriter(file, stderr)
	}
	logger := slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level}))
	return logger, func() { _ = file.Close() }, nil
}

// logFile writes to one file and rotates it when it grows past size. Rotated
// files are the file name with a numeric suffix, the highest suffix is dropped.
type logFile struct {
	path string
	size int64
	max  int64
	keep int

	mu   sync.Mutex
	file *os.File
}

// openLogFile opens path, creating the directory when missing, and appends to
// the file when it exists.
func openLogFile(path string, limit int64, keep int) (*logFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create the log directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: open the log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("daemon: read the log file size: %w", err)
	}
	return &logFile{path: path, size: info.Size(), max: limit, keep: keep, file: file}, nil
}

// Write appends p to the log file, rotating it first when it would grow past
// the limit.
func (f *logFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.file == nil {
		return 0, errors.New("daemon: log file is closed")
	}
	if f.max > 0 && f.size > 0 && f.size+int64(len(p)) > f.max {
		if err := f.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := f.file.Write(p)
	f.size += int64(n)
	return n, err
}

// Close closes the log file.
func (f *logFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.file == nil {
		return nil
	}
	file := f.file
	f.file = nil
	return file.Close()
}

// rotate closes the current file, shifts the rotated files by one and opens an
// empty file under the same name.
func (f *logFile) rotate() error {
	if err := f.file.Close(); err != nil {
		return fmt.Errorf("daemon: close the log file for rotation: %w", err)
	}
	f.file = nil

	if err := os.Remove(f.path + "." + strconv.Itoa(f.keep)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("daemon: remove the oldest log file: %w", err)
	}
	for i := f.keep - 1; i >= 1; i-- {
		from := f.path + "." + strconv.Itoa(i)
		to := f.path + "." + strconv.Itoa(i+1)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("daemon: rename a rotated log file: %w", err)
		}
	}
	if err := os.Rename(f.path, f.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("daemon: rename the log file: %w", err)
	}

	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: open the new log file: %w", err)
	}
	f.file = file
	f.size = 0
	return nil
}
