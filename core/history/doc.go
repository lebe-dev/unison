// Package history keeps the clipboard history of a device.
//
// Every device records the items it captures and the items it receives from the
// Personal peers in one append-only log keyed by the origin device and its
// sequence number. A manual deletion adds a tombstone to the log, so the
// deletion reaches the other devices with the same exchange as the items.
// Expiry is decided by the origin device and recorded in the item: an entry
// that is past its expiry is hidden from every query and removed by the
// periodic cleanup, so it disappears on every device without a network.
//
// Both sides of a new session with a Personal peer send their version vector
// and answer the vector of the other side with the entries it misses. Payloads
// do not travel with the exchange, only their descriptions.
package history
