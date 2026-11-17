package download

import (
	"errors"
	"os"
	"strings"
)

// marksFiles reports whether Mark leaves a mark on this platform.
const marksFiles = true

// isMarked reports whether the file carries the zone identifier of the
// internet zone.
func isMarked(path string) (bool, error) {
	data, err := os.ReadFile(path + ":Zone.Identifier")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(data), "ZoneId=3"), nil
}
