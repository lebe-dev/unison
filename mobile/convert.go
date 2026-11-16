package mobile

import (
	"encoding/hex"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/pairing"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
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

// eventKindToProto converts a node event kind to its local API form.
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
	case node.EventTransfer:
		return localv1.EventKind_EVENT_KIND_TRANSFER
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
	default:
		return localv1.EventKind_EVENT_KIND_UNSPECIFIED
	}
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

// pairingMethodToProto converts a pairing method to its local API form.
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

// localEventToProto converts a node event to its local API form.
func localEventToProto(event node.Event) *localv1.Event {
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
		ProtocolVersion: event.Version,
		AtUnixMs:        unixMilli(event.At),
		PairingMethod:   pairingMethodToProto(event.Method),
	}
	switch event.Kind {
	case node.EventClipboard, node.EventClipDownload, node.EventClipApply:
		out.Item = clipItemToProto(event.Item)
	}
	if event.Err != nil {
		out.Error = event.Err.Error()
	}
	return out
}

// eventToMobile converts a node event to the form delivered to the host.
func eventToMobile(event node.Event) *mobilev1.Event {
	return &mobilev1.Event{
		Type:     eventTypeOf(event.Kind),
		TsUnixMs: unixMilli(event.At),
		Event:    localEventToProto(event),
	}
}

// eventTypeOf converts a node event kind to the mobile event type.
func eventTypeOf(kind node.EventKind) mobilev1.EventType {
	switch kind {
	case node.EventPeerOnline:
		return mobilev1.EventType_EVENT_TYPE_PEER_ONLINE
	case node.EventPeerOffline:
		return mobilev1.EventType_EVENT_TYPE_PEER_OFFLINE
	case node.EventClipboard:
		return mobilev1.EventType_EVENT_TYPE_CLIP_RECEIVED
	case node.EventPeerIncompatible:
		return mobilev1.EventType_EVENT_TYPE_PEER_INCOMPATIBLE
	case node.EventPairingCandidate:
		return mobilev1.EventType_EVENT_TYPE_PAIRING_CANDIDATE
	case node.EventPairingRequest:
		return mobilev1.EventType_EVENT_TYPE_PAIRING_REQUEST
	case node.EventPairingPaired:
		return mobilev1.EventType_EVENT_TYPE_PAIRING_PAIRED
	case node.EventPairingFailed:
		return mobilev1.EventType_EVENT_TYPE_PAIRING_FAILED
	case node.EventPairingClosed:
		return mobilev1.EventType_EVENT_TYPE_PAIRING_CLOSED
	default:
		return mobilev1.EventType_EVENT_TYPE_UNSPECIFIED
	}
}
