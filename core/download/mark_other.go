//go:build !darwin && !windows

package download

// Mark does nothing on a platform without a mark for downloaded files.
func Mark(string) error {
	return nil
}
