// Package node wires the core packages into a running device: the database, the
// identity, the trust store, the transport, the sessions with the paired
// devices, the discovery announcement and the clipboard pipeline. On top of them
// it runs the connection manager, the pairing window and the event bus that the
// daemon, the local API and the mobile facade use.
package node
