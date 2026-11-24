package mediate

import (
	"context"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
)

// Direction tells which side of an exchange the local device is.
type Direction uint8

// Directions of an exchange.
const (
	// DirectionMediator reports an exchange this device started for two other
	// devices.
	DirectionMediator Direction = iota + 1
	// DirectionIncoming reports an exchange another device started for this
	// device and a third one.
	DirectionIncoming
)

// String returns the name of the direction.
func (d Direction) String() string {
	switch d {
	case DirectionMediator:
		return "mediator"
	case DirectionIncoming:
		return "incoming"
	default:
		return "unknown"
	}
}

// State is the progress of an exchange.
type State uint8

// States of an exchange.
const (
	// StateWaiting reports an exchange that waits for the users.
	StateWaiting State = iota + 1
	// StateReady reports that one endpoint accepted and waits for the other.
	StateReady
	// StateAwaitingFinish reports that the lower endpoint sent its keys.
	StateAwaitingFinish
	// StateCompleted reports a pair that was written to the trust store.
	StateCompleted
	// StateCanceled reports an exchange that ended without a new pair.
	StateCanceled
)

// String returns the name of the state.
func (s State) String() string {
	switch s {
	case StateWaiting:
		return "waiting"
	case StateReady:
		return "ready"
	case StateAwaitingFinish:
		return "awaiting-finish"
	case StateCompleted:
		return "completed"
	case StateCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// CancelReason tells why an exchange ended without a new pair.
type CancelReason uint8

// Reasons of an ended exchange.
const (
	// CancelReasonNone reports an exchange that did not end.
	CancelReasonNone CancelReason = iota
	// CancelReasonUserRejected reports a user who declined.
	CancelReasonUserRejected
	// CancelReasonMediatorCanceled reports a mediator that canceled.
	CancelReasonMediatorCanceled
	// CancelReasonExpired reports an exchange past its deadline.
	CancelReasonExpired
	// CancelReasonRevoked reports a revoked device.
	CancelReasonRevoked
	// CancelReasonRoleMismatch reports a device paired with another role.
	CancelReasonRoleMismatch
	// CancelReasonAlreadyPaired reports devices that are paired and online.
	CancelReasonAlreadyPaired
	// CancelReasonBusy reports the limit of active exchanges.
	CancelReasonBusy
	// CancelReasonPeerOffline reports an endpoint that lost its session.
	CancelReasonPeerOffline
	// CancelReasonFailed reports a message that did not pass validation.
	CancelReasonFailed
)

// String returns the name of the reason.
func (r CancelReason) String() string {
	switch r {
	case CancelReasonUserRejected:
		return "user-rejected"
	case CancelReasonMediatorCanceled:
		return "mediator-canceled"
	case CancelReasonExpired:
		return "expired"
	case CancelReasonRevoked:
		return "revoked"
	case CancelReasonRoleMismatch:
		return "role-mismatch"
	case CancelReasonAlreadyPaired:
		return "already-paired"
	case CancelReasonBusy:
		return "busy"
	case CancelReasonPeerOffline:
		return "peer-offline"
	case CancelReasonFailed:
		return "failed"
	default:
		return "none"
	}
}

// Device describes one endpoint of an exchange.
type Device struct {
	// ID is the device ID.
	ID model.DeviceID
	// Name is the display name known to this device.
	Name string
	// Platform is the operating system of the device.
	Platform model.Platform
}

// Exchange is a snapshot of one mediated pairing exchange.
type Exchange struct {
	// SessionID is the identifier of the exchange in lowercase hex.
	SessionID string
	// Direction tells which side of the exchange this device is.
	Direction Direction
	// State is the progress of the exchange.
	State State
	// Role is the role of the new pair.
	Role model.Role
	// Mediator is the device that runs the exchange.
	Mediator Device
	// PeerA and PeerB are the endpoints an exchange of the mediator introduces.
	PeerA Device
	PeerB Device
	// Peer is the other endpoint of an incoming exchange.
	Peer Device
	// SAS is the verification code both endpoints show.
	SAS pairing.SAS
	// AlreadyPaired reports a peer that is stored already and was offline at
	// the offer, so a confirmation replaces its connection keys.
	AlreadyPaired bool
	// ExpiresAt is the deadline of the exchange.
	ExpiresAt time.Time
	// CancelReason tells why an exchange ended without a new pair.
	CancelReason CancelReason
	// EnableFriendsDiscovery records that the user asked to turn the search
	// for Friends devices on when the pair completes.
	EnableFriendsDiscovery bool
}

// EventKind classifies a mediate event.
type EventKind uint8

// Mediate event kinds.
const (
	// EventOffer reports an incoming offer that waits for the user.
	EventOffer EventKind = iota + 1
	// EventUpdated reports a change of an active exchange.
	EventUpdated
	// EventCompleted reports a pair written to the trust store.
	EventCompleted
	// EventCanceled reports an exchange that ended without a new pair.
	EventCanceled
)

// String returns the name of the event kind.
func (k EventKind) String() string {
	switch k {
	case EventOffer:
		return "offer"
	case EventUpdated:
		return "updated"
	case EventCompleted:
		return "completed"
	case EventCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// Event is one change of an exchange.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// Exchange is the snapshot of the exchange.
	Exchange Exchange
}

// EventSink receives the events of the service.
type EventSink interface {
	// Report delivers one event. It must not block.
	Report(ctx context.Context, event Event)
}
