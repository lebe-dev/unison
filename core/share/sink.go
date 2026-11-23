package share

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/lebe-dev/unison/core/download"
)

// downloadSink writes received files into the downloads directory.
type downloadSink struct {
	// dir returns the directory of the files.
	dir func(ctx context.Context) (string, error)
}

// Write stores one file and returns its path.
func (d downloadSink) Write(ctx context.Context, file File, src io.Reader) (string, error) {
	dir, err := d.dir(ctx)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("share: no downloads directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("share: create downloads directory: %w", err)
	}

	path, target, err := download.Create(dir, file.Name)
	if err != nil {
		return "", fmt.Errorf("share: %w", err)
	}
	written, err := io.Copy(target, src)
	closeErr := target.Close()
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("share: write file: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("share: close file: %w", closeErr)
	}
	if written != file.Size {
		_ = os.Remove(path)
		return "", fmt.Errorf("%w: wrote %d bytes of %d", ErrInvalidFile, written, file.Size)
	}
	return path, nil
}

// checkName reports a file name that cannot be used.
func checkName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: empty name", ErrInvalidFile)
	case !utf8.ValidString(name):
		return fmt.Errorf("%w: name is not valid UTF-8", ErrInvalidFile)
	case len(name) > MaxNameLength:
		return fmt.Errorf("%w: name of %d bytes", ErrInvalidFile, len(name))
	}
	return nil
}
