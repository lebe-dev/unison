//go:build !darwin && !windows

package download

// marksFiles reports whether Mark leaves a mark on this platform.
const marksFiles = false

// isMarked reports that a file carries no mark on this platform.
func isMarked(string) (bool, error) {
	return false, nil
}
