//go:build darwin && !cgo

package daemon

// cgoAvailable reports that the Keychain and the pasteboard are unreachable,
// because the package that uses them was built without cgo.
const cgoAvailable = false
