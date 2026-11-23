package download

import (
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

// marksFiles reports whether Mark leaves a mark on this platform.
const marksFiles = true

// isMarked reports whether the file carries the quarantine attribute of a
// download.
func isMarked(path string) (bool, error) {
	buffer := make([]byte, 256)
	size, err := unix.Getxattr(path, quarantineAttribute, buffer)
	if errors.Is(err, unix.ENOATTR) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(buffer[:size]), ";Unison;"), nil
}
