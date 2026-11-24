package api

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/mediate"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/trust"
	"github.com/lebe-dev/unison/internal/observability"
)

// errorOf maps an error of the core to a connect error. The message of the
// cause is kept, it never carries clipboard content or secrets. An internal
// failure is reported, the client errors are expected and are not.
func errorOf(err error) *connect.Error {
	if err == nil {
		return nil
	}
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		if connectErr.Code() == connect.CodeInternal {
			observability.CaptureException(err)
		}
		return connectErr
	}
	code := codeOf(err)
	if code == connect.CodeInternal {
		observability.CaptureException(err)
	}
	return connect.NewError(code, err)
}

// codeOf maps an error of the core to a connect code.
func codeOf(err error) connect.Code {
	switch {
	case errors.Is(err, context.Canceled):
		return connect.CodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return connect.CodeDeadlineExceeded
	case errors.Is(err, ErrNotRunning),
		errors.Is(err, node.ErrStopped),
		errors.Is(err, node.ErrStarted),
		errors.Is(err, node.ErrPairingOpen),
		errors.Is(err, node.ErrPairingClosed),
		errors.Is(err, pairing.ErrClosed),
		errors.Is(err, pairing.ErrDecided),
		errors.Is(err, pairing.ErrPending),
		errors.Is(err, pairing.ErrRoleMismatch),
		errors.Is(err, share.ErrPeerOffline),
		errors.Is(err, share.ErrNotPending),
		errors.Is(err, share.ErrEnded),
		errors.Is(err, share.ErrTooManyPending),
		errors.Is(err, mediate.ErrBusy),
		errors.Is(err, mediate.ErrRateLimited),
		errors.Is(err, mediate.ErrRoleEscalation),
		errors.Is(err, mediate.ErrRoleMismatch),
		errors.Is(err, mediate.ErrAlreadyPaired),
		errors.Is(err, mediate.ErrExpired),
		errors.Is(err, mediate.ErrState),
		errors.Is(err, mediate.ErrUnsupportedPeer):
		return connect.CodeFailedPrecondition
	case errors.Is(err, pairing.ErrBusy):
		return connect.CodeResourceExhausted
	case errors.Is(err, pairing.ErrUnsupported):
		return connect.CodeUnimplemented
	case errors.Is(err, pairing.ErrRejected),
		errors.Is(err, pairing.ErrPeerRevoked),
		errors.Is(err, pairing.ErrToken),
		errors.Is(err, trust.ErrPeerRevoked),
		errors.Is(err, mediate.ErrPeerRevoked):
		return connect.CodePermissionDenied
	case errors.Is(err, pairing.ErrUnknownRequest),
		errors.Is(err, trust.ErrPeerNotFound),
		errors.Is(err, trust.ErrSecretNotFound),
		errors.Is(err, trust.ErrRevocationNotFound),
		errors.Is(err, trust.ErrIntroductionNotFound),
		errors.Is(err, history.ErrNotFound),
		errors.Is(err, share.ErrNotFound),
		errors.Is(err, mediate.ErrNotFound):
		return connect.CodeNotFound
	case errors.Is(err, trust.ErrPeerExists):
		return connect.CodeAlreadyExists
	case errors.Is(err, pairing.ErrInvalidPeer),
		errors.Is(err, pairing.ErrQR),
		errors.Is(err, trust.ErrInvalidPeer),
		errors.Is(err, trust.ErrInvalidSecret),
		errors.Is(err, clip.ErrUnsupportedContent),
		errors.Is(err, history.ErrInvalidItem),
		errors.Is(err, share.ErrNoFiles),
		errors.Is(err, share.ErrNoPeers),
		errors.Is(err, share.ErrTooManyFiles),
		errors.Is(err, share.ErrInvalidFile),
		errors.Is(err, mediate.ErrInvalidOffer),
		errors.Is(err, mediate.ErrInvalidCommit),
		errors.Is(err, mediate.ErrInvalidFinish):
		return connect.CodeInvalidArgument
	case errors.Is(err, history.ErrUnsupported):
		return connect.CodeUnimplemented
	case errors.Is(err, history.ErrUnavailable),
		errors.Is(err, clip.ErrPayloadMissing):
		return connect.CodeFailedPrecondition
	case errors.Is(err, node.ErrPeerOffline),
		errors.Is(err, mediate.ErrOffline),
		errors.Is(err, pairing.ErrUnreachable):
		return connect.CodeUnavailable
	case errors.Is(err, history.ErrNoSource):
		return connect.CodeUnavailable
	default:
		return connect.CodeInternal
	}
}
