//go:build darwin && cgo

package daemon

// cgoAvailable reports that the Keychain and the pasteboard are reachable.
const cgoAvailable = true
