package trust

import "github.com/lebe-dev/unison/core/model"

// Action is one row of the capability matrix: an interaction a peer may
// trigger.
type Action uint8

// Peer actions.
const (
	// ActionNone is the zero value and is never allowed.
	ActionNone Action = iota
	// ActionReceiveClipAnnounce lets the peer announce clipboard items.
	ActionReceiveClipAnnounce
	// ActionApplyClipboard lets announcements of the peer reach the local
	// clipboard without a prompt.
	ActionApplyClipboard
	// ActionSyncHistory lets the peer exchange clip history.
	ActionSyncHistory
	// ActionFetchBlob lets the peer download blobs referenced by the history.
	ActionFetchBlob
	// ActionReceiveShare lets the peer offer files through a share.
	ActionReceiveShare
	// ActionReceiveFiles lets the peer send files to the local device.
	ActionReceiveFiles
	// ActionIntroduce lets the peer introduce other Personal devices.
	ActionIntroduce
	// ActionRevoke lets the peer revoke devices of the group.
	ActionRevoke
	// ActionSetRemotePolicy lets the peer change the local peer policy.
	ActionSetRemotePolicy
)

// String returns the name of the action.
func (a Action) String() string {
	switch a {
	case ActionReceiveClipAnnounce:
		return "receive-clip-announce"
	case ActionApplyClipboard:
		return "apply-clipboard"
	case ActionSyncHistory:
		return "sync-history"
	case ActionFetchBlob:
		return "fetch-blob"
	case ActionReceiveShare:
		return "receive-share"
	case ActionReceiveFiles:
		return "receive-files"
	case ActionIntroduce:
		return "introduce"
	case ActionRevoke:
		return "revoke"
	case ActionSetRemotePolicy:
		return "set-remote-policy"
	default:
		return "none"
	}
}

// Decision is the outcome of the capability matrix.
type Decision uint8

// Matrix outcomes.
const (
	// Deny forbids the action.
	Deny Decision = iota
	// Allow permits the action.
	Allow
	// Confirm permits the action only after the user confirms it. Callers that
	// act immediately must treat it as Deny; Allowed already does.
	Confirm
)

// String returns the name of the decision.
func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Confirm:
		return "confirm"
	default:
		return "deny"
	}
}

// matrixRow holds the outcome of one action for every peer column.
type matrixRow struct {
	// scope is the confirmation flag that moves a Personal peer to the
	// personalConfirmation column. An action without a scope ignores the flags.
	scope                Scope
	personal             Decision
	personalConfirmation Decision
	friends              Decision
}

// matrix holds the outcome of every action. An action missing here is denied.
var matrix = map[Action]matrixRow{
	ActionReceiveClipAnnounce: {personal: Allow, personalConfirmation: Allow, friends: Deny},
	ActionApplyClipboard:      {scope: ScopeClipboard, personal: Allow, personalConfirmation: Confirm, friends: Deny},
	ActionSyncHistory:         {personal: Allow, personalConfirmation: Allow, friends: Deny},
	ActionFetchBlob:           {personal: Allow, personalConfirmation: Allow, friends: Deny},
	ActionReceiveShare:        {scope: ScopeFiles, personal: Allow, personalConfirmation: Confirm, friends: Confirm},
	ActionReceiveFiles:        {scope: ScopeFiles, personal: Allow, personalConfirmation: Confirm, friends: Confirm},
	ActionIntroduce:           {personal: Allow, personalConfirmation: Allow, friends: Deny},
	ActionRevoke:              {personal: Allow, personalConfirmation: Allow, friends: Deny},
	ActionSetRemotePolicy:     {personal: Allow, personalConfirmation: Allow, friends: Deny},
}

// Allowed reports whether the peer may perform the action without asking the
// user. It fails closed: a revoked peer, an unknown role, an unknown action and
// an action that requires confirmation are all reported as false. A caller that
// can ask the user checks Decision for Confirm.
func (p Peer) Allowed(action Action) bool {
	return p.Decision(action) == Allow
}

// Decision returns the matrix outcome of the action for the peer.
func (p Peer) Decision(action Action) Decision {
	if p.Revoked {
		return Deny
	}
	row, ok := matrix[action]
	if !ok {
		return Deny
	}

	switch p.Role {
	case model.RolePersonal:
		if p.Confirm.Get(row.scope) {
			return row.personalConfirmation
		}
		return row.personal
	case model.RoleFriends:
		return row.friends
	default:
		return Deny
	}
}
