//go:build windows

package daemon

// removeStaleSocket does nothing: a named pipe disappears with its last
// handle, so there is no stale endpoint to clean up.
func removeStaleSocket(string) error { return nil }
