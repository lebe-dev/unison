// Package protoconv converts domain types to and from protobuf messages.
package protoconv

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lebe-dev/unison/core/model"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// blake3Size is the digest size carried by a representation.
const blake3Size = 32

// UnixMilli converts t to unix milliseconds. The zero time maps to 0.
func UnixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// TimeFromUnixMilli converts unix milliseconds to a time. 0 maps to the zero time.
func TimeFromUnixMilli(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// RoleToProto converts a role to its wire form.
func RoleToProto(r model.Role) wirev1.Role {
	switch r {
	case model.RolePersonal:
		return wirev1.Role_ROLE_PERSONAL
	case model.RoleFriends:
		return wirev1.Role_ROLE_FRIENDS
	default:
		return wirev1.Role_ROLE_UNSPECIFIED
	}
}

// RoleFromProto converts a wire role to its domain form.
func RoleFromProto(r wirev1.Role) model.Role {
	switch r {
	case wirev1.Role_ROLE_PERSONAL:
		return model.RolePersonal
	case wirev1.Role_ROLE_FRIENDS:
		return model.RoleFriends
	default:
		return model.RoleUnspecified
	}
}

// PlatformToProto converts a platform to its wire form.
func PlatformToProto(p model.Platform) wirev1.Platform {
	switch p {
	case model.PlatformMacOS:
		return wirev1.Platform_PLATFORM_MACOS
	case model.PlatformWindows:
		return wirev1.Platform_PLATFORM_WINDOWS
	case model.PlatformAndroid:
		return wirev1.Platform_PLATFORM_ANDROID
	default:
		return wirev1.Platform_PLATFORM_UNSPECIFIED
	}
}

// PlatformFromProto converts a wire platform to its domain form.
func PlatformFromProto(p wirev1.Platform) model.Platform {
	switch p {
	case wirev1.Platform_PLATFORM_MACOS:
		return model.PlatformMacOS
	case wirev1.Platform_PLATFORM_WINDOWS:
		return model.PlatformWindows
	case wirev1.Platform_PLATFORM_ANDROID:
		return model.PlatformAndroid
	default:
		return model.PlatformUnknown
	}
}

// KindToProto converts a content kind to its wire form.
func KindToProto(k model.Kind) wirev1.Kind {
	switch k {
	case model.KindText:
		return wirev1.Kind_KIND_TEXT
	case model.KindImage:
		return wirev1.Kind_KIND_IMAGE
	case model.KindFiles:
		return wirev1.Kind_KIND_FILES
	default:
		return wirev1.Kind_KIND_UNSPECIFIED
	}
}

// KindFromProto converts a wire content kind to its domain form.
func KindFromProto(k wirev1.Kind) model.Kind {
	switch k {
	case wirev1.Kind_KIND_TEXT:
		return model.KindText
	case wirev1.Kind_KIND_IMAGE:
		return model.KindImage
	case wirev1.Kind_KIND_FILES:
		return model.KindFiles
	default:
		return model.KindUnspecified
	}
}

// SourceToProto converts a source to its wire form.
func SourceToProto(s model.Source) wirev1.Source {
	switch s {
	case model.SourceClipboard:
		return wirev1.Source_SOURCE_CLIPBOARD
	case model.SourceShare:
		return wirev1.Source_SOURCE_SHARE
	case model.SourceCLI:
		return wirev1.Source_SOURCE_CLI
	default:
		return wirev1.Source_SOURCE_UNSPECIFIED
	}
}

// SourceFromProto converts a wire source to its domain form.
func SourceFromProto(s wirev1.Source) model.Source {
	switch s {
	case wirev1.Source_SOURCE_CLIPBOARD:
		return model.SourceClipboard
	case wirev1.Source_SOURCE_SHARE:
		return model.SourceShare
	case wirev1.Source_SOURCE_CLI:
		return model.SourceCLI
	default:
		return model.SourceUnspecified
	}
}

// RepresentationToProto converts a representation to its wire form.
func RepresentationToProto(r model.Representation) *wirev1.Representation {
	return &wirev1.Representation{
		MimeType:    r.MIME,
		Size:        r.Size,
		Blake3:      r.Hash[:],
		InlineBytes: slices.Clone(r.Inline),
		FileName:    r.FileName,
		MtimeUnixMs: UnixMilli(r.MTime),
	}
}

// RepresentationFromProto converts a wire representation to its domain form.
func RepresentationFromProto(r *wirev1.Representation) (model.Representation, error) {
	if r == nil {
		return model.Representation{}, errors.New("representation is nil")
	}
	if len(r.GetBlake3()) != blake3Size {
		return model.Representation{}, fmt.Errorf(
			"representation %q: hash is %d bytes, want %d",
			r.GetMimeType(), len(r.GetBlake3()), blake3Size,
		)
	}
	rep := model.Representation{
		MIME:     r.GetMimeType(),
		Size:     r.GetSize(),
		Inline:   slices.Clone(r.GetInlineBytes()),
		FileName: r.GetFileName(),
		MTime:    TimeFromUnixMilli(r.GetMtimeUnixMs()),
	}
	copy(rep.Hash[:], r.GetBlake3())
	return rep, nil
}

// ClipItemToProto converts a clip item to its wire form.
func ClipItemToProto(item model.ClipItem) *wirev1.ClipItem {
	reps := make([]*wirev1.Representation, 0, len(item.Representations))
	for _, r := range item.Representations {
		reps = append(reps, RepresentationToProto(r))
	}
	return &wirev1.ClipItem{
		Id:              item.ID[:],
		Origin:          item.Origin[:],
		Seq:             item.Seq,
		CreatedAtUnixMs: UnixMilli(item.CreatedAt),
		ExpiresAtUnixMs: UnixMilli(item.ExpiresAt),
		Kind:            KindToProto(item.Kind),
		Representations: reps,
		Source:          SourceToProto(item.Source),
		Deleted:         item.Deleted,
	}
}

// ClipItemFromProto converts a wire clip item to its domain form.
func ClipItemFromProto(item *wirev1.ClipItem) (model.ClipItem, error) {
	if item == nil {
		return model.ClipItem{}, errors.New("clip item is nil")
	}
	var out model.ClipItem
	if len(item.GetId()) != len(out.ID) {
		return model.ClipItem{}, fmt.Errorf("clip item id: got %d bytes, want %d", len(item.GetId()), len(out.ID))
	}
	copy(out.ID[:], item.GetId())
	origin, err := model.DeviceIDFromBytes(item.GetOrigin())
	if err != nil {
		return model.ClipItem{}, fmt.Errorf("clip item origin: %w", err)
	}
	out.Origin = origin
	out.Seq = item.GetSeq()
	out.CreatedAt = TimeFromUnixMilli(item.GetCreatedAtUnixMs())
	out.ExpiresAt = TimeFromUnixMilli(item.GetExpiresAtUnixMs())
	out.Kind = KindFromProto(item.GetKind())
	out.Source = SourceFromProto(item.GetSource())
	out.Deleted = item.GetDeleted()
	// The entries are converted one by one: a peer may send many entries that
	// fail conversion, and the slice must grow only with the ones kept.
	for i, r := range item.GetRepresentations() {
		rep, err := RepresentationFromProto(r)
		if err != nil {
			return model.ClipItem{}, fmt.Errorf("clip item representation %d: %w", i, err)
		}
		out.Representations = append(out.Representations, rep)
	}
	return out, nil
}

// NetworkStateToMobile converts a network state to its mobile form.
func NetworkStateToMobile(s model.NetworkState) *mobilev1.NetworkState {
	return &mobilev1.NetworkState{
		Online:    s.Online,
		Metered:   s.Metered,
		Roaming:   s.Roaming,
		Vpn:       s.VPN,
		DataSaver: s.DataSaver,
	}
}

// NetworkStateFromMobile converts a mobile network state to its domain form.
// The mobile message carries no transport type, so WiFi and Cellular stay false.
func NetworkStateFromMobile(s *mobilev1.NetworkState) model.NetworkState {
	if s == nil {
		return model.NetworkState{}
	}
	return model.NetworkState{
		Online:    s.GetOnline(),
		Metered:   s.GetMetered(),
		Roaming:   s.GetRoaming(),
		VPN:       s.GetVpn(),
		DataSaver: s.GetDataSaver(),
	}
}
