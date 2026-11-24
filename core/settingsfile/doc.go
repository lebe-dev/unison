// Package settingsfile encodes the settings of a device as a JSON document and
// reads them back.
//
// The document is versioned. A newer app reads an older document through the
// migrations of every version it passed, and an older app reads a newer
// document by applying the keys it knows.
package settingsfile
