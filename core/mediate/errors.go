package mediate

import "errors"

// Errors reported by the service.
var (
	// ErrRoleEscalation reports a role above the role the mediator has.
	ErrRoleEscalation = errors.New("mediate: role escalation")
	// ErrPeerRevoked reports a revoked device.
	ErrPeerRevoked = errors.New("mediate: peer is revoked")
	// ErrRoleMismatch reports a device already paired with another role.
	ErrRoleMismatch = errors.New("mediate: role mismatch")
	// ErrAlreadyPaired reports two devices that are paired and online.
	ErrAlreadyPaired = errors.New("mediate: devices are already paired")
	// ErrBusy reports the limit of active exchanges.
	ErrBusy = errors.New("mediate: too many exchanges")
	// ErrRateLimited reports a mediator that offers too often.
	ErrRateLimited = errors.New("mediate: rate limited")
	// ErrUnsupportedPeer reports a device without the capability.
	ErrUnsupportedPeer = errors.New("mediate: peer does not support mediated pairing")
	// ErrNotFound reports an unknown or finished exchange.
	ErrNotFound = errors.New("mediate: exchange not found")
	// ErrExpired reports an exchange past its deadline.
	ErrExpired = errors.New("mediate: exchange expired")
	// ErrInvalidOffer reports an offer that does not pass validation.
	ErrInvalidOffer = errors.New("mediate: invalid offer")
	// ErrInvalidCommit reports a commit that does not pass validation.
	ErrInvalidCommit = errors.New("mediate: invalid commit")
	// ErrInvalidFinish reports a finish that does not pass validation.
	ErrInvalidFinish = errors.New("mediate: invalid finish")
	// ErrOffline reports an endpoint without an open session.
	ErrOffline = errors.New("mediate: peer is offline")
	// ErrState reports a confirmation of an exchange that cannot take it.
	ErrState = errors.New("mediate: exchange is not waiting for a decision")
)
