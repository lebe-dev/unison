package journal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Writer appends to one file and rotates it when it grows past the limit.
// Rotated files are the file name with a numeric suffix, the highest suffix is
// dropped.
type Writer struct {
	path string
	size int64
	max  int64
	keep int

	mu   sync.Mutex
	file *os.File
}

// OpenWriter opens path, creating the directory when missing, and appends to
// the file when it exists.
func OpenWriter(path string, limit int64, keep int) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("journal: create the log directory: %w", err)
	}

	//nolint:gosec // The log file lives in the data directory of the application.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("journal: open the log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("journal: read the log file size: %w", err)
	}
	return &Writer{path: path, size: info.Size(), max: limit, keep: keep, file: file}, nil
}

// Write appends p to the log file, rotating it first when it would grow past
// the limit.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, errors.New("journal: log file is closed")
	}
	if w.max > 0 && w.size > 0 && w.size+int64(len(p)) > w.max {
		// A failed rotation keeps the records in the current file, which grows
		// past the limit until the next rotation succeeds. Losing the log for
		// the rest of the run would hide every later failure.
		if err := w.rotate(); err != nil && w.file == nil {
			if reopenErr := w.reopen(); reopenErr != nil {
				return 0, errors.Join(err, reopenErr)
			}
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the log file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	file := w.file
	w.file = nil
	return file.Close()
}

// reopen opens the current file again for appending after a failed rotation
// closed it.
func (w *Writer) reopen() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("journal: reopen the log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("journal: read the log file size: %w", err)
	}
	w.file = file
	w.size = info.Size()
	return nil
}

// rotate closes the current file, shifts the rotated files by one and opens an
// empty file under the same name.
func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("journal: close the log file for rotation: %w", err)
	}
	w.file = nil

	if err := os.Remove(w.path + "." + strconv.Itoa(w.keep)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("journal: remove the oldest log file: %w", err)
	}
	for i := w.keep - 1; i >= 1; i-- {
		from := w.path + "." + strconv.Itoa(i)
		to := w.path + "." + strconv.Itoa(i+1)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("journal: rename a rotated log file: %w", err)
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("journal: rename the log file: %w", err)
	}

	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("journal: open the new log file: %w", err)
	}
	w.file = file
	w.size = 0
	return nil
}
