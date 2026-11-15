package mobile

import "errors"

// Platform is implemented by the Android app. It receives callbacks from the core
// on the core's own goroutines.
type Platform interface {
	// OnEvent receives a serialized event.
	OnEvent(evt []byte)
	// WriteClipboard writes an item to the system clipboard.
	WriteClipboard(item []byte)
	// OpenBlobSink returns a file descriptor for an incoming blob.
	OpenBlobSink(meta []byte) int
	// KeystoreUnwrap decrypts a key blob with the platform keystore.
	KeystoreUnwrap(blob []byte) []byte
}

// version is the version of the core, set at link time.
var version = "dev"

// Start prepares the core with its configuration and the platform callbacks.
func Start(_ []byte, p Platform) error {
	if p == nil {
		return errors.New("platform is nil")
	}

	return nil
}

// Stop releases the resources held by the core.
func Stop() {}

// Version returns the version of the core.
func Version() string {
	return version
}
