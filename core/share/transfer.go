package share

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

// Limits and names of the package.
const (
	// MaxFiles bounds the files of one transfer.
	MaxFiles = 32
	// MaxTransfers bounds the transfers the service keeps. The oldest finished
	// transfer is dropped first.
	MaxTransfers = 64
	// MaxPending bounds the incoming offers that wait for a decision.
	MaxPending = 16
	// MaxPendingPerPeer bounds the incoming offers of one peer that wait for a
	// decision.
	MaxPendingPerPeer = 8
	// PendingTTL is how long an offer waits for a decision.
	PendingTTL = 10 * time.Minute
	// MaxNameLength bounds a file name in bytes.
	MaxNameLength = 200

	// PayloadRequest is the payload type of a share blob request stream.
	PayloadRequest = "share.request"

	// SettingDownloadsDir is the settings key of the directory that receives
	// shared files.
	SettingDownloadsDir = "share.downloads_dir"

	// maxAttempts bounds the downloads of one file that fail while the peer is
	// still connected.
	maxAttempts = 3
	// progressEvery is the number of chunks between two progress events.
	progressEvery = 16
)

// Errors reported by the service.
var (
	// ErrNotFound reports a transfer ID the service does not know.
	ErrNotFound = errors.New("share: transfer not found")
	// ErrPeerOffline reports a receiver without an open session.
	ErrPeerOffline = errors.New("share: peer is offline")
	// ErrNoFiles reports a transfer without files.
	ErrNoFiles = errors.New("share: no files")
	// ErrNoPeers reports a request without receivers.
	ErrNoPeers = errors.New("share: no receivers")
	// ErrTooManyFiles reports a transfer above MaxFiles.
	ErrTooManyFiles = errors.New("share: too many files")
	// ErrInvalidFile reports a file name, size or hash the service refuses.
	ErrInvalidFile = errors.New("share: invalid file")
	// ErrNotPending reports a decision on an offer that is not waiting for one.
	ErrNotPending = errors.New("share: offer does not wait for a decision")
	// ErrTooManyPending reports offers that pile up without a decision.
	ErrTooManyPending = errors.New("share: too many offers wait for a decision")
	// ErrEnded reports a transfer that already reached its final state.
	ErrEnded = errors.New("share: transfer has ended")
)

// Direction of a transfer.
type Direction uint8

// Transfer directions.
const (
	DirectionUnknown Direction = iota
	// DirectionOutgoing is a transfer this device offers.
	DirectionOutgoing
	// DirectionIncoming is a transfer a peer offers to this device.
	DirectionIncoming
)

// String returns the name of the direction.
func (d Direction) String() string {
	switch d {
	case DirectionOutgoing:
		return "outgoing"
	case DirectionIncoming:
		return "incoming"
	default:
		return "unknown"
	}
}

// State of a transfer.
type State uint8

// Transfer states.
const (
	StateUnknown State = iota
	// StatePending is an incoming offer that waits for the local decision.
	StatePending
	// StateOffered is an outgoing offer the peer has not answered yet.
	StateOffered
	// StateTransferring moves the files.
	StateTransferring
	// StateDone finished the transfer.
	StateDone
	// StateDeclined is an offer the receiver declined.
	StateDeclined
	// StateCanceled is a transfer either side stopped.
	StateCanceled
	// StateFailed is a transfer that could not finish.
	StateFailed
)

// String returns the name of the state.
func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateOffered:
		return "offered"
	case StateTransferring:
		return "transferring"
	case StateDone:
		return "done"
	case StateDeclined:
		return "declined"
	case StateCanceled:
		return "canceled"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ended reports a state a transfer does not leave.
func (s State) ended() bool {
	switch s {
	case StateDone, StateDeclined, StateCanceled, StateFailed:
		return true
	default:
		return false
	}
}

// File is one file of a transfer.
type File struct {
	// Name is the file name shown to the user and used on the disk.
	Name string
	// Size is the plaintext size in bytes.
	Size int64
	// Hash is the BLAKE3 hash of the content.
	Hash [32]byte
	// MTime is the modification time of the source file.
	MTime time.Time
	// MIME is the content type guessed from the name, empty when unknown.
	MIME string
}

// Transfer is the state of one transfer.
type Transfer struct {
	// ID is the identifier of the transfer, chosen by the sender.
	ID string
	// Direction is the side of this device.
	Direction Direction
	// Peer is the device at the other side.
	Peer model.DeviceID
	// PeerName is the display name of the peer.
	PeerName string
	// State is the phase of the transfer.
	State State
	// Files are the files of the transfer.
	Files []File
	// Bytes is the number of bytes that moved.
	Bytes uint64
	// TotalBytes is the size of all files.
	TotalBytes uint64
	// Paths are the saved files of a finished incoming transfer.
	Paths []string
	// Err is the failure in plain English.
	Err string
	// CreatedAt is the time the transfer started.
	CreatedAt time.Time
	// ExpiresAt is the time an offer that waits for a decision stops waiting.
	// It is zero for the other states.
	ExpiresAt time.Time
}

// clone returns a copy that does not share the slices of the receiver.
func (t Transfer) clone() Transfer {
	out := t
	out.Files = append([]File(nil), t.Files...)
	out.Paths = append([]string(nil), t.Paths...)
	return out
}

// totalBytes returns the size of all files.
func totalBytes(files []File) uint64 {
	var total uint64
	for _, file := range files {
		total += uint64(max(file.Size, 0))
	}
	return total
}

// DefaultDownloadsDir returns the directory that receives shared files.
func DefaultDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads", "Unison")
}
