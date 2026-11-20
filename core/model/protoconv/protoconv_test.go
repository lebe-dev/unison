package protoconv

import (
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

func TestTimeConversions(t *testing.T) {
	if got := UnixMilli(time.Time{}); got != 0 {
		t.Fatalf("zero time: got %d, want 0", got)
	}
	if got := TimeFromUnixMilli(0); !got.IsZero() {
		t.Fatalf("zero ms: got %v, want the zero time", got)
	}

	at := time.Date(2026, 11, 15, 10, 20, 30, 0, time.UTC)
	if got, want := UnixMilli(at), at.UnixMilli(); got != want {
		t.Fatalf("unix milli: got %d, want %d", got, want)
	}
	if got := TimeFromUnixMilli(UnixMilli(at)); !got.Equal(at) {
		t.Fatalf("round trip: got %v, want %v", got, at)
	}
}

func TestEnumConversions(t *testing.T) {
	roles := map[model.Role]wirev1.Role{
		model.RoleUnspecified: wirev1.Role_ROLE_UNSPECIFIED,
		model.RolePersonal:    wirev1.Role_ROLE_PERSONAL,
		model.RoleFriends:     wirev1.Role_ROLE_FRIENDS,
	}
	for domain, proto := range roles {
		if got := RoleToProto(domain); got != proto {
			t.Errorf("role to proto %v: got %v, want %v", domain, got, proto)
		}
		if got := RoleFromProto(proto); got != domain {
			t.Errorf("role from proto %v: got %v, want %v", proto, got, domain)
		}
	}

	platforms := map[model.Platform]wirev1.Platform{
		model.PlatformUnknown: wirev1.Platform_PLATFORM_UNSPECIFIED,
		model.PlatformMacOS:   wirev1.Platform_PLATFORM_MACOS,
		model.PlatformWindows: wirev1.Platform_PLATFORM_WINDOWS,
		model.PlatformAndroid: wirev1.Platform_PLATFORM_ANDROID,
		model.PlatformLinux:   wirev1.Platform_PLATFORM_LINUX,
	}
	for domain, proto := range platforms {
		if got := PlatformToProto(domain); got != proto {
			t.Errorf("platform to proto %v: got %v, want %v", domain, got, proto)
		}
		if got := PlatformFromProto(proto); got != domain {
			t.Errorf("platform from proto %v: got %v, want %v", proto, got, domain)
		}
	}

	kinds := map[model.Kind]wirev1.Kind{
		model.KindUnspecified: wirev1.Kind_KIND_UNSPECIFIED,
		model.KindText:        wirev1.Kind_KIND_TEXT,
		model.KindImage:       wirev1.Kind_KIND_IMAGE,
		model.KindFiles:       wirev1.Kind_KIND_FILES,
	}
	for domain, proto := range kinds {
		if got := KindToProto(domain); got != proto {
			t.Errorf("kind to proto %v: got %v, want %v", domain, got, proto)
		}
		if got := KindFromProto(proto); got != domain {
			t.Errorf("kind from proto %v: got %v, want %v", proto, got, domain)
		}
	}

	sources := map[model.Source]wirev1.Source{
		model.SourceUnspecified: wirev1.Source_SOURCE_UNSPECIFIED,
		model.SourceClipboard:   wirev1.Source_SOURCE_CLIPBOARD,
		model.SourceShare:       wirev1.Source_SOURCE_SHARE,
		model.SourceCLI:         wirev1.Source_SOURCE_CLI,
	}
	for domain, proto := range sources {
		if got := SourceToProto(domain); got != proto {
			t.Errorf("source to proto %v: got %v, want %v", domain, got, proto)
		}
		if got := SourceFromProto(proto); got != domain {
			t.Errorf("source from proto %v: got %v, want %v", proto, got, domain)
		}
	}
}

func TestUnknownEnums(t *testing.T) {
	if got := RoleFromProto(wirev1.Role(99)); got != model.RoleUnspecified {
		t.Errorf("unknown role: got %v", got)
	}
	if got := PlatformFromProto(wirev1.Platform(99)); got != model.PlatformUnknown {
		t.Errorf("unknown platform: got %v", got)
	}
	if got := KindFromProto(wirev1.Kind(99)); got != model.KindUnspecified {
		t.Errorf("unknown kind: got %v", got)
	}
	if got := SourceFromProto(wirev1.Source(99)); got != model.SourceUnspecified {
		t.Errorf("unknown source: got %v", got)
	}
}

func TestClipItemRoundTrip(t *testing.T) {
	var origin model.DeviceID
	origin[0] = 7
	var id model.ClipID
	id[0] = 9
	var hash [32]byte
	hash[0] = 3

	created := time.Date(2026, 11, 15, 10, 20, 30, 0, time.UTC)
	item := model.ClipItem{
		ID:        id,
		Origin:    origin,
		Seq:       42,
		CreatedAt: created,
		ExpiresAt: created.Add(7 * 24 * time.Hour),
		Kind:      model.KindFiles,
		Representations: []model.Representation{{
			MIME:     "application/x-unison-file",
			Size:     1024,
			Hash:     hash,
			FileName: "notes.txt",
			MTime:    created,
		}},
		Source:  model.SourceShare,
		Deleted: true,
	}

	got, err := ClipItemFromProto(ClipItemToProto(item))
	if err != nil {
		t.Fatalf("from proto: %v", err)
	}
	if got.ID != item.ID || got.Origin != item.Origin || got.Seq != item.Seq {
		t.Fatalf("identity fields: got %+v, want %+v", got, item)
	}
	if !got.CreatedAt.Equal(item.CreatedAt) || !got.ExpiresAt.Equal(item.ExpiresAt) {
		t.Fatalf("times: got %v - %v, want %v - %v", got.CreatedAt, got.ExpiresAt, item.CreatedAt, item.ExpiresAt)
	}
	if got.Kind != item.Kind || got.Source != item.Source || got.Deleted != item.Deleted {
		t.Fatalf("metadata: got %+v, want %+v", got, item)
	}
	if len(got.Representations) != 1 {
		t.Fatalf("representations: got %d, want 1", len(got.Representations))
	}
	rep := got.Representations[0]
	if rep.MIME != "application/x-unison-file" || rep.Size != 1024 || rep.Hash != hash || rep.FileName != "notes.txt" {
		t.Fatalf("representation: got %+v", rep)
	}
	if !rep.MTime.Equal(item.Representations[0].MTime) {
		t.Fatalf("representation mtime: got %v, want %v", rep.MTime, item.Representations[0].MTime)
	}
}

func TestInlineRepresentation(t *testing.T) {
	var hash [32]byte
	payload := []byte("hello")
	proto := RepresentationToProto(model.Representation{MIME: "text/plain", Size: 5, Hash: hash, Inline: payload})

	rep, err := RepresentationFromProto(proto)
	if err != nil {
		t.Fatalf("from proto: %v", err)
	}
	if string(rep.Inline) != "hello" {
		t.Fatalf("inline: got %q, want %q", rep.Inline, "hello")
	}

	proto.InlineBytes[0] = 'x'
	if string(rep.Inline) != "hello" {
		t.Fatal("inline payload aliases the proto message")
	}
}

func TestClipItemFromProtoRejectsMalformed(t *testing.T) {
	valid := ClipItemToProto(model.ClipItem{
		ID:     model.ClipID{1},
		Origin: model.DeviceID{1},
	})

	cases := map[string]*wirev1.ClipItem{
		"nil":          nil,
		"short id":     {Origin: valid.Origin},
		"short origin": {Id: valid.Id},
	}
	for name, proto := range cases {
		if _, err := ClipItemFromProto(proto); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	badHash := ClipItemToProto(model.ClipItem{ID: model.ClipID{1}, Origin: model.DeviceID{1}})
	badHash.Representations = []*wirev1.Representation{{MimeType: "text/plain", Blake3: []byte{1}}}
	if _, err := ClipItemFromProto(badHash); err == nil {
		t.Error("bad representation hash: expected an error")
	}
}

func TestNetworkStateMobile(t *testing.T) {
	state := model.NetworkState{
		Online:    true,
		Metered:   true,
		Roaming:   true,
		VPN:       true,
		DataSaver: true,
	}
	proto := NetworkStateToMobile(state)
	if !proto.GetOnline() || !proto.GetMetered() || !proto.GetRoaming() || !proto.GetVpn() || !proto.GetDataSaver() {
		t.Fatalf("to mobile: got %+v", proto)
	}
	if got := NetworkStateFromMobile(proto); got != state {
		t.Fatalf("from mobile: got %+v, want %+v", got, state)
	}
	if got := NetworkStateFromMobile(nil); got != (model.NetworkState{}) {
		t.Fatalf("nil mobile state: got %+v", got)
	}
}

func TestNetworkStateMobileHasNoTransport(t *testing.T) {
	state := model.NetworkState{WiFi: true, Cellular: true}
	proto := NetworkStateToMobile(state)
	if got := NetworkStateFromMobile(proto); got.WiFi || got.Cellular {
		t.Fatalf("transport type survived conversion: %+v", got)
	}
}
