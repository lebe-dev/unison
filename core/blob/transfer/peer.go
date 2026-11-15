package transfer

import (
	"context"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/session"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Sender sends control messages to a peer.
type Sender interface {
	// Send writes one control message to the peer.
	Send(ctx context.Context, msg *wirev1.ControlMessage) error
}

// Peer is the part of a session the transfer layer uses. *session.Session
// implements it.
type Peer interface {
	Sender
	// DeviceID returns the device ID of the peer.
	DeviceID() model.DeviceID
	// OpenRequest opens a request stream with header as its first frame.
	OpenRequest(ctx context.Context, header *wirev1.StreamHeader) (*session.Request, error)
}

var _ Peer = (*session.Session)(nil)

// ShareAuthorizer decides whether a peer may fetch a blob of a share. It is
// asked only for request streams that carry a transfer ID.
type ShareAuthorizer interface {
	// AllowBlob reports whether peer may fetch blob hash of the share
	// transferID.
	AllowBlob(ctx context.Context, peer model.DeviceID, transferID string, hash [32]byte) bool
}
