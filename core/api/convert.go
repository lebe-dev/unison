package api

import (
	"encoding/hex"
	"errors"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/trust"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// unixMilli converts t to unix milliseconds. The zero time maps to 0.
func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// roleToProto converts a role to its local API form.
func roleToProto(r model.Role) localv1.Role {
	switch r {
	case model.RolePersonal:
		return localv1.Role_ROLE_PERSONAL
	case model.RoleFriends:
		return localv1.Role_ROLE_FRIENDS
	default:
		return localv1.Role_ROLE_UNSPECIFIED
	}
}

// roleFromProto converts a local API role to its domain form.
func roleFromProto(r localv1.Role) model.Role {
	switch r {
	case localv1.Role_ROLE_PERSONAL:
		return model.RolePersonal
	case localv1.Role_ROLE_FRIENDS:
		return model.RoleFriends
	default:
		return model.RoleUnspecified
	}
}

// platformToProto converts a platform to its local API form.
func platformToProto(p model.Platform) localv1.Platform {
	switch p {
	case model.PlatformMacOS:
		return localv1.Platform_PLATFORM_MACOS
	case model.PlatformWindows:
		return localv1.Platform_PLATFORM_WINDOWS
	case model.PlatformAndroid:
		return localv1.Platform_PLATFORM_ANDROID
	default:
		return localv1.Platform_PLATFORM_UNSPECIFIED
	}
}

// pathToProto converts a network path to its local API form.
func pathToProto(p model.Path) localv1.Path {
	switch p {
	case model.PathLAN:
		return localv1.Path_PATH_LAN
	case model.PathRelay:
		return localv1.Path_PATH_RELAY
	default:
		return localv1.Path_PATH_UNSPECIFIED
	}
}

// kindToProto converts a content kind to its local API form.
func kindToProto(k model.Kind) localv1.Kind {
	switch k {
	case model.KindText:
		return localv1.Kind_KIND_TEXT
	case model.KindImage:
		return localv1.Kind_KIND_IMAGE
	case model.KindFiles:
		return localv1.Kind_KIND_FILES
	default:
		return localv1.Kind_KIND_UNSPECIFIED
	}
}

// kindFromProto converts a local API kind to its domain form.
func kindFromProto(k localv1.Kind) model.Kind {
	switch k {
	case localv1.Kind_KIND_TEXT:
		return model.KindText
	case localv1.Kind_KIND_IMAGE:
		return model.KindImage
	case localv1.Kind_KIND_FILES:
		return model.KindFiles
	default:
		return model.KindUnspecified
	}
}

// sourceToProto converts a capture source to its local API form.
func sourceToProto(s model.Source) localv1.Source {
	switch s {
	case model.SourceClipboard:
		return localv1.Source_SOURCE_CLIPBOARD
	case model.SourceShare:
		return localv1.Source_SOURCE_SHARE
	case model.SourceCLI:
		return localv1.Source_SOURCE_CLI
	default:
		return localv1.Source_SOURCE_UNSPECIFIED
	}
}

// networkToProto converts the network conditions of the device.
func networkToProto(state model.NetworkState) *localv1.NetworkState {
	return &localv1.NetworkState{
		Online:    state.Online,
		Wifi:      state.WiFi,
		Cellular:  state.Cellular,
		Metered:   state.Metered,
		Roaming:   state.Roaming,
		Vpn:       state.VPN,
		DataSaver: state.DataSaver,
	}
}

// deviceToProto converts a stored peer and its live state.
func deviceToProto(peer trust.Peer, state model.PeerState) *localv1.Device {
	device := &localv1.Device{
		DeviceId:                    peer.DeviceID().String(),
		Name:                        peer.Name,
		Role:                        roleToProto(peer.Role),
		Platform:                    platformToProto(peer.Platform),
		Online:                      state.Online,
		Path:                        pathToProto(state.Path),
		RequireConfirmation:         peer.Confirm.Clipboard,
		RequireFileConfirmation:     peer.Confirm.Files,
		PeerRequireConfirmation:     peer.ConfirmRemote.Clipboard,
		PeerRequireFileConfirmation: peer.ConfirmRemote.Files,
		AllowRemoteSettings:         !peer.RemoteSettingsDenied,
		Revoked:                     peer.Revoked,
		AddedAtUnixMs:               unixMilli(peer.AddedAt),
		LastSeenAtUnixMs:            unixMilli(peer.LastSeenAt),
		Unreachable:                 state.Unreachable,
		UnreachableSinceUnixMs:      unixMilli(state.UnreachableSince),
		UnreachableDetail:           state.UnreachableDetail,
	}
	if !peer.IntroducedBy.IsZero() {
		device.IntroducedBy = peer.IntroducedBy.String()
	}
	return device
}

// representationToProto converts one payload representation.
func representationToProto(r model.Representation) *localv1.Representation {
	rep := &localv1.Representation{
		MimeType:    r.MIME,
		Size:        r.Size,
		InlineBytes: append([]byte(nil), r.Inline...),
		FileName:    r.FileName,
		MtimeUnixMs: unixMilli(r.MTime),
	}
	if r.Hash != [32]byte{} {
		rep.Blake3 = hex.EncodeToString(r.Hash[:])
	}
	return rep
}

// clipItemToProto converts a clipboard item.
func clipItemToProto(item model.ClipItem) *localv1.ClipItem {
	out := &localv1.ClipItem{
		Id:              item.ID.String(),
		Origin:          item.Origin.String(),
		Seq:             item.Seq,
		CreatedAtUnixMs: unixMilli(item.CreatedAt),
		ExpiresAtUnixMs: unixMilli(item.ExpiresAt),
		Kind:            kindToProto(item.Kind),
		Source:          sourceToProto(item.Source),
		Deleted:         item.Deleted,
	}
	for _, r := range item.Representations {
		out.Representations = append(out.Representations, representationToProto(r))
	}
	return out
}

// sasToProto converts a short authentication string.
func sasToProto(sas pairing.SAS) *localv1.Sas {
	if sas.IsZero() {
		return nil
	}
	return &localv1.Sas{Digits: sas.Digits, Emoji: append([]string(nil), sas.Emoji...)}
}

// eventKindToProto converts a node event kind.
func eventKindToProto(kind node.EventKind) localv1.EventKind {
	switch kind {
	case node.EventPeerOnline:
		return localv1.EventKind_EVENT_KIND_PEER_ONLINE
	case node.EventPeerOffline:
		return localv1.EventKind_EVENT_KIND_PEER_OFFLINE
	case node.EventClipboard:
		return localv1.EventKind_EVENT_KIND_CLIPBOARD
	case node.EventClipDownload:
		return localv1.EventKind_EVENT_KIND_CLIP_DOWNLOAD
	case node.EventClipApply:
		return localv1.EventKind_EVENT_KIND_CLIP_APPLY
	case node.EventPeerIncompatible:
		return localv1.EventKind_EVENT_KIND_PEER_INCOMPATIBLE
	case node.EventPairingCandidate:
		return localv1.EventKind_EVENT_KIND_PAIRING_CANDIDATE
	case node.EventPairingRequest:
		return localv1.EventKind_EVENT_KIND_PAIRING_REQUEST
	case node.EventPairingPaired:
		return localv1.EventKind_EVENT_KIND_PAIRING_PAIRED
	case node.EventPairingFailed:
		return localv1.EventKind_EVENT_KIND_PAIRING_FAILED
	case node.EventPairingClosed:
		return localv1.EventKind_EVENT_KIND_PAIRING_CLOSED
	case node.EventTransfer:
		return localv1.EventKind_EVENT_KIND_TRANSFER
	case node.EventDeviceUpdated:
		return localv1.EventKind_EVENT_KIND_DEVICE_UPDATED
	case node.EventPeerUnreachable:
		return localv1.EventKind_EVENT_KIND_PEER_UNREACHABLE
	default:
		return localv1.EventKind_EVENT_KIND_UNSPECIFIED
	}
}

// pairingMethodToProto converts a pairing method.
func pairingMethodToProto(method pairing.Method) localv1.PairingMethod {
	switch method {
	case pairing.MethodSAS:
		return localv1.PairingMethod_PAIRING_METHOD_SAS
	case pairing.MethodQR:
		return localv1.PairingMethod_PAIRING_METHOD_QR
	default:
		return localv1.PairingMethod_PAIRING_METHOD_UNSPECIFIED
	}
}

// eventToProto converts a node event.
func eventToProto(event node.Event) *localv1.Event {
	out := &localv1.Event{
		Kind:            eventKindToProto(event.Kind),
		DeviceId:        event.Peer.String(),
		Name:            event.Name,
		Role:            roleToProto(event.Role),
		Platform:        platformToProto(event.Platform),
		Path:            pathToProto(event.Path),
		Local:           event.Local,
		Addr:            event.Addr,
		Sas:             sasToProto(event.SAS),
		PairingMethod:   pairingMethodToProto(event.Method),
		ProtocolVersion: event.Version,
		AtUnixMs:        unixMilli(event.At),
	}
	switch event.Kind {
	case node.EventClipboard, node.EventClipDownload, node.EventClipApply:
		out.Item = clipItemToProto(event.Item)
	case node.EventTransfer:
		out.Transfer = TransferToProto(event.Transfer)
	}
	if event.Err != nil {
		out.Error = event.Err.Error()
		out.FailureReason = failureReasonOf(event)
	}
	return out
}

// failureReasonOf classifies the failure of an event for the short message of
// a client.
func failureReasonOf(event node.Event) localv1.FailureReason {
	if event.Kind == node.EventPeerUnreachable || errors.Is(event.Err, pairing.ErrUnreachable) {
		return localv1.FailureReason_FAILURE_REASON_UNREACHABLE
	}
	return localv1.FailureReason_FAILURE_REASON_UNSPECIFIED
}
