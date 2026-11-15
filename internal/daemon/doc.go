// Package daemon runs the Unison daemon: it prepares the data directory, holds
// the single instance lock, opens the log file, starts the node and serves the
// local API on the endpoint of the data directory.
package daemon
