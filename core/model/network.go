package model

import "time"

// NetworkState describes the current network conditions of the local device.
type NetworkState struct {
	// Online reports whether a network is available.
	Online bool
	// WiFi reports whether the active network is Wi-Fi.
	WiFi bool
	// Cellular reports whether the active network is cellular.
	Cellular bool
	// Metered reports whether traffic is billed by volume.
	Metered bool
	// Roaming reports whether the device is roaming.
	Roaming bool
	// VPN reports whether a VPN is active.
	VPN bool
	// DataSaver reports whether the system data saver is on.
	DataSaver bool
}

// Path is the network path used to reach a peer.
type Path uint8

// Peer network paths.
const (
	// PathUnknown is the zero value and marks an unknown path.
	PathUnknown Path = iota
	// PathLAN is a direct local network connection.
	PathLAN
	// PathRelay is a connection through the relay.
	PathRelay
)

// String returns the lower case name of the path.
func (p Path) String() string {
	switch p {
	case PathLAN:
		return "lan"
	case PathRelay:
		return "relay"
	default:
		return "unknown"
	}
}

// PeerState is the live connection state of a paired device.
type PeerState struct {
	// DeviceID identifies the peer.
	DeviceID DeviceID
	// Online reports whether a session with the peer is active.
	Online bool
	// Path is the network path in use.
	Path Path
	// Unreachable reports an offline peer that did not answer on any of its
	// known addresses at the last attempt: it is turned off, or the network
	// keeps the two devices apart.
	Unreachable bool
	// UnreachableSince is when the peer stopped answering.
	UnreachableSince time.Time
	// UnreachableDetail lists the addresses of the last attempt and why each
	// one failed, one per line, for the technical details of the UI.
	UnreachableDetail string
}
