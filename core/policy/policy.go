package policy

import (
	"fmt"

	"github.com/lebe-dev/unison/core/model"
)

// Action is what happens to an incoming item.
type Action uint8

// Item outcomes.
const (
	// ActionReject drops the item: it is not recorded and not shown.
	ActionReject Action = iota
	// ActionMetadataOnly records the item and leaves fetching to the user.
	ActionMetadataOnly
	// ActionConfirm records the item and asks the user before applying it.
	ActionConfirm
	// ActionAutoApply applies the item without asking.
	ActionAutoApply
)

// String returns the name of the action.
func (a Action) String() string {
	switch a {
	case ActionMetadataOnly:
		return "metadata"
	case ActionConfirm:
		return "confirm"
	case ActionAutoApply:
		return "auto"
	default:
		return "reject"
	}
}

// ParseAction parses the name of an action.
func ParseAction(s string) (Action, error) {
	switch s {
	case "reject":
		return ActionReject, nil
	case "metadata":
		return ActionMetadataOnly, nil
	case "confirm":
		return ActionConfirm, nil
	case "auto":
		return ActionAutoApply, nil
	default:
		return ActionReject, fmt.Errorf("policy: unknown action %q", s)
	}
}

// Condition is the network condition that selects a policy row.
type Condition uint8

// Network conditions, from the most permissive to the most restrictive.
const (
	// ConditionUnknown is the zero value and is never matched by a row.
	ConditionUnknown Condition = iota
	// ConditionUnmetered is an online network without restrictions.
	ConditionUnmetered
	// ConditionMetered is a network billed by volume.
	ConditionMetered
	// ConditionRoaming is a roaming network.
	ConditionRoaming
	// ConditionDataSaver is a network with the system data saver on.
	ConditionDataSaver
	// ConditionOffline is a device without a network.
	ConditionOffline
)

// String returns the settings name of the condition.
func (c Condition) String() string {
	switch c {
	case ConditionUnmetered:
		return "unmetered"
	case ConditionMetered:
		return "metered"
	case ConditionRoaming:
		return "roaming"
	case ConditionDataSaver:
		return "data_saver"
	case ConditionOffline:
		return "offline"
	default:
		return "unknown"
	}
}

// Conditions returns the conditions that have a configurable row, from the most
// permissive to the most restrictive.
func Conditions() []Condition {
	return []Condition{ConditionUnmetered, ConditionMetered, ConditionRoaming, ConditionDataSaver}
}

// Kinds returns the content kinds that have a configurable cell.
func Kinds() []model.Kind {
	return []model.Kind{model.KindText, model.KindImage, model.KindFiles}
}

// ConditionOf maps the network state of the device to a policy condition. A
// restriction always wins over a weaker one, and a device without a network is
// reported as offline. A state without restriction flags is unmetered, which
// covers wired networks.
func ConditionOf(state model.NetworkState) Condition {
	if !state.Online {
		return ConditionOffline
	}
	switch {
	case state.DataSaver:
		return ConditionDataSaver
	case state.Roaming:
		return ConditionRoaming
	case state.Metered:
		return ConditionMetered
	default:
		return ConditionUnmetered
	}
}

// Peer is the trust state of the peer that sent an item.
type Peer struct {
	// Role is the trust role of the peer.
	Role model.Role
	// RequireConfirmation marks a Personal peer whose items are not applied
	// without user confirmation.
	RequireConfirmation bool
}

// Input describes one incoming item.
type Input struct {
	// Peer is the peer that sent the item.
	Peer Peer
	// Network is the current network state of the local device.
	Network model.NetworkState
	// Path is the connection path used to reach the peer.
	Path model.Path
	// Kind is the content kind of the item.
	Kind model.Kind
	// Size is the total payload size of the item in bytes.
	Size int64
	// Source is what triggered the local decision.
	Source model.Source
}

// Result is the outcome for one item.
type Result struct {
	// Action is what happens to the item.
	Action Action
	// Eager reports that the payload is fetched before the item is applied.
	Eager bool
	// Relay reports that the relay path may be used for the peer.
	Relay bool
}

// Decide returns the outcome for in under cfg. It fails closed: an unknown
// role, kind, source or path never grants an automatic apply.
func Decide(cfg Config, in Input) Result {
	if !valid(in) {
		return Result{Action: ActionReject}
	}
	if in.Peer.Role == model.RoleFriends && in.Source == model.SourceClipboard {
		return Result{Action: ActionReject}
	}

	row, ok := cfg.Row(ConditionOf(in.Network))
	if ok && in.Path == model.PathRelay && !row.Relay {
		return Result{Action: ActionReject}
	}

	relay := ok && in.Network.Online && row.Relay
	if in.Source == model.SourceCLI {
		// The local user asked for the item, so it is applied without asking
		// again; the payload is fetched only while the device is online.
		return Result{Action: ActionAutoApply, Eager: in.Network.Online, Relay: relay}
	}
	if !ok {
		// No network means no transfer: the item stays in the history.
		if asksConfirmation(in.Peer) {
			return Result{Action: ActionConfirm}
		}
		return Result{Action: ActionMetadataOnly}
	}
	if asksConfirmation(in.Peer) {
		return Result{Action: ActionConfirm, Relay: relay}
	}

	cell, ok := row.Cell(in.Kind)
	if !ok {
		return Result{Action: ActionMetadataOnly, Relay: relay}
	}
	limit := eagerLimit(cfg, cell, in.Path)
	if limit < 0 {
		return Result{Action: ActionMetadataOnly, Relay: relay}
	}

	action := cell.Action
	if in.Source == model.SourceShare && action == ActionMetadataOnly {
		// A share is an explicit offer of the peer, so the clipboard default
		// of the cell does not apply; the size limit still does.
		action = ActionAutoApply
	}
	if action == ActionConfirm {
		return Result{Action: ActionConfirm, Relay: relay}
	}
	if action != ActionAutoApply {
		return Result{Action: action, Relay: relay}
	}
	if in.Size > limit {
		return Result{Action: ActionMetadataOnly, Relay: relay}
	}
	return Result{Action: ActionAutoApply, Eager: true, Relay: relay}
}

// eagerLimit returns the largest item fetched eagerly on path, or a negative
// value when the path does not allow a transfer.
func eagerLimit(cfg Config, cell Cell, path model.Path) int64 {
	switch path {
	case model.PathLAN:
		return cell.MaxSize
	case model.PathRelay:
		return min(cell.MaxSize, cfg.RelayMaxSize)
	default:
		return -1
	}
}

// asksConfirmation reports whether the user must confirm the item.
func asksConfirmation(p Peer) bool {
	return p.RequireConfirmation || p.Role == model.RoleFriends
}

// valid reports whether every enum of in is a known value.
func valid(in Input) bool {
	switch in.Peer.Role {
	case model.RolePersonal, model.RoleFriends:
	default:
		return false
	}
	switch in.Kind {
	case model.KindText, model.KindImage, model.KindFiles:
	default:
		return false
	}
	switch in.Source {
	case model.SourceClipboard, model.SourceShare, model.SourceCLI:
	default:
		return false
	}
	return in.Size >= 0
}
