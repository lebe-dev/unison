package policy

import (
	"testing"

	"github.com/lebe-dev/unison/core/model"
)

// Network states used by the table tests.
var (
	wifi     = model.NetworkState{Online: true, WiFi: true}
	cellular = model.NetworkState{Online: true, Cellular: true, Metered: true}
	roaming  = model.NetworkState{Online: true, Cellular: true, Metered: true, Roaming: true}
	saver    = model.NetworkState{Online: true, WiFi: true, DataSaver: true}
	offline  = model.NetworkState{}
)

const (
	smallSize = 1 << 10
	megabyte  = 1 << 20
)

// personal is a Personal peer without the confirmation flag.
var personal = Peer{Role: model.RolePersonal}

// TestDecideTable covers every cell of the network policy table with a small
// item, which is under every default size limit.
func TestDecideTable(t *testing.T) {
	tests := []struct {
		name    string
		network model.NetworkState
		kind    model.Kind
		want    Action
		relay   bool
	}{
		{name: "wifi text", network: wifi, kind: model.KindText, want: ActionAutoApply, relay: true},
		{name: "wifi image", network: wifi, kind: model.KindImage, want: ActionAutoApply, relay: true},
		{name: "wifi files", network: wifi, kind: model.KindFiles, want: ActionMetadataOnly, relay: true},

		{name: "metered text", network: cellular, kind: model.KindText, want: ActionAutoApply, relay: true},
		{name: "metered image", network: cellular, kind: model.KindImage, want: ActionAutoApply, relay: true},
		{name: "metered files", network: cellular, kind: model.KindFiles, want: ActionMetadataOnly, relay: true},

		{name: "roaming text", network: roaming, kind: model.KindText, want: ActionAutoApply, relay: false},
		{name: "roaming image", network: roaming, kind: model.KindImage, want: ActionMetadataOnly, relay: false},
		{name: "roaming files", network: roaming, kind: model.KindFiles, want: ActionMetadataOnly, relay: false},

		{name: "data saver text", network: saver, kind: model.KindText, want: ActionAutoApply, relay: false},
		{name: "data saver image", network: saver, kind: model.KindImage, want: ActionMetadataOnly, relay: false},
		{name: "data saver files", network: saver, kind: model.KindFiles, want: ActionMetadataOnly, relay: false},

		{name: "offline text", network: offline, kind: model.KindText, want: ActionMetadataOnly, relay: false},
		{name: "offline image", network: offline, kind: model.KindImage, want: ActionMetadataOnly, relay: false},
		{name: "offline files", network: offline, kind: model.KindFiles, want: ActionMetadataOnly, relay: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Decide(Default(), Input{
				Peer:    personal,
				Network: test.network,
				Path:    model.PathLAN,
				Kind:    test.kind,
				Size:    smallSize,
				Source:  model.SourceClipboard,
			})
			if got.Action != test.want {
				t.Fatalf("action: got %s, want %s", got.Action, test.want)
			}
			if got.Relay != test.relay {
				t.Fatalf("relay: got %t, want %t", got.Relay, test.relay)
			}
			if want := test.want == ActionAutoApply; got.Eager != want {
				t.Fatalf("eager: got %t, want %t", got.Eager, want)
			}
		})
	}
}

// TestDecideEagerThresholds covers the eager limit of a Personal device on the
// local network and over the relay.
func TestDecideEagerThresholds(t *testing.T) {
	tests := []struct {
		name    string
		network model.NetworkState
		path    model.Path
		kind    model.Kind
		size    int64
		want    Action
	}{
		{name: "lan at limit", network: wifi, path: model.PathLAN, kind: model.KindImage, size: DefaultLANMaxSize, want: ActionAutoApply},
		{name: "lan above limit", network: wifi, path: model.PathLAN, kind: model.KindImage, size: DefaultLANMaxSize + 1, want: ActionMetadataOnly},
		{name: "lan text at limit", network: wifi, path: model.PathLAN, kind: model.KindText, size: DefaultLANMaxSize, want: ActionAutoApply},
		{name: "lan text above limit", network: wifi, path: model.PathLAN, kind: model.KindText, size: DefaultLANMaxSize + 1, want: ActionMetadataOnly},

		{name: "metered at limit", network: cellular, path: model.PathLAN, kind: model.KindImage, size: DefaultRelayMaxSize, want: ActionAutoApply},
		{name: "metered above limit", network: cellular, path: model.PathLAN, kind: model.KindImage, size: DefaultRelayMaxSize + 1, want: ActionMetadataOnly},
		{name: "metered text above limit", network: cellular, path: model.PathLAN, kind: model.KindText, size: DefaultRelayMaxSize + 1, want: ActionMetadataOnly},

		{name: "relay at limit", network: wifi, path: model.PathRelay, kind: model.KindImage, size: DefaultRelayMaxSize, want: ActionAutoApply},
		{name: "relay above limit", network: wifi, path: model.PathRelay, kind: model.KindImage, size: DefaultRelayMaxSize + 1, want: ActionMetadataOnly},
		{name: "relay under lan limit", network: wifi, path: model.PathRelay, kind: model.KindText, size: DefaultLANMaxSize, want: ActionMetadataOnly},
		{name: "metered relay", network: cellular, path: model.PathRelay, kind: model.KindImage, size: DefaultRelayMaxSize, want: ActionAutoApply},
		{name: "relay above metered row", network: cellular, path: model.PathRelay, kind: model.KindImage, size: 2 * DefaultRelayMaxSize, want: ActionMetadataOnly},

		{name: "roaming text above limit", network: roaming, path: model.PathLAN, kind: model.KindText, size: DefaultRelayMaxSize + 1, want: ActionMetadataOnly},
		{name: "data saver text above limit", network: saver, path: model.PathLAN, kind: model.KindText, size: DefaultRelayMaxSize + 1, want: ActionMetadataOnly},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Decide(Default(), Input{
				Peer:    personal,
				Network: test.network,
				Path:    test.path,
				Kind:    test.kind,
				Size:    test.size,
				Source:  model.SourceClipboard,
			})
			if got.Action != test.want {
				t.Fatalf("action: got %s, want %s", got.Action, test.want)
			}
		})
	}
}

func TestDecideRelayPath(t *testing.T) {
	cfg := Default()
	tests := []struct {
		name    string
		network model.NetworkState
		want    Action
	}{
		{name: "wifi allows relay", network: wifi, want: ActionAutoApply},
		{name: "metered allows relay", network: cellular, want: ActionAutoApply},
		{name: "roaming forbids relay", network: roaming, want: ActionReject},
		{name: "data saver forbids relay", network: saver, want: ActionReject},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Decide(cfg, Input{
				Peer:    personal,
				Network: test.network,
				Path:    model.PathRelay,
				Kind:    model.KindText,
				Size:    smallSize,
				Source:  model.SourceClipboard,
			})
			if got.Action != test.want {
				t.Fatalf("action: got %s, want %s", got.Action, test.want)
			}
			if got.Action == ActionReject && got.Relay {
				t.Fatal("rejected item reports the relay as allowed")
			}
		})
	}
}

func TestDecideFilesOverRelay(t *testing.T) {
	share := Input{
		Peer:    personal,
		Network: wifi,
		Path:    model.PathRelay,
		Kind:    model.KindFiles,
		Size:    smallSize,
		Source:  model.SourceShare,
	}

	kept := Decide(Default(), share)
	if kept.Action != ActionMetadataOnly || !kept.Relay {
		t.Fatalf("a shared file over the relay: %+v", kept)
	}

	allowed := Default()
	allowed.RelayFilesOnRequest = false
	if got := Decide(allowed, share); got.Action != ActionAutoApply {
		t.Fatalf("a shared file with the rule off: %s", got.Action)
	}

	asked := share
	asked.Source = model.SourceCLI
	if got := Decide(Default(), asked); got.Action != ActionAutoApply {
		t.Fatalf("a file the user asked for: %s", got.Action)
	}

	lan := share
	lan.Path = model.PathLAN
	if got := Decide(Default(), lan); got.Action != ActionAutoApply {
		t.Fatalf("a shared file in the local network: %s", got.Action)
	}
}

func TestDecidePeer(t *testing.T) {
	confirmation := Peer{Role: model.RolePersonal, RequireConfirmation: true}
	friends := Peer{Role: model.RoleFriends}

	tests := []struct {
		name    string
		peer    Peer
		network model.NetworkState
		source  model.Source
		kind    model.Kind
		size    int64
		want    Action
	}{
		{name: "personal applies", peer: personal, source: model.SourceClipboard, kind: model.KindText, size: smallSize, want: ActionAutoApply},
		{name: "personal confirmation", peer: confirmation, source: model.SourceClipboard, kind: model.KindText, size: smallSize, want: ActionConfirm},
		{name: "personal confirmation beats size", peer: confirmation, source: model.SourceClipboard, kind: model.KindImage, size: DefaultLANMaxSize + 1, want: ActionConfirm},
		{name: "personal confirmation share", peer: confirmation, source: model.SourceShare, kind: model.KindFiles, size: smallSize, want: ActionConfirm},
		{name: "personal confirmation explicit", peer: confirmation, source: model.SourceCLI, kind: model.KindFiles, size: DefaultLANMaxSize, want: ActionAutoApply},
		{name: "friends announcement", peer: friends, source: model.SourceClipboard, kind: model.KindText, size: smallSize, want: ActionReject},
		{name: "friends share", peer: friends, source: model.SourceShare, kind: model.KindFiles, size: smallSize, want: ActionConfirm},
		{name: "friends explicit", peer: friends, source: model.SourceCLI, kind: model.KindFiles, size: smallSize, want: ActionAutoApply},
		{name: "personal share files", peer: personal, source: model.SourceShare, kind: model.KindFiles, size: smallSize, want: ActionAutoApply},
		{name: "personal share files above limit", peer: personal, source: model.SourceShare, kind: model.KindFiles, size: DefaultLANMaxSize + 1, want: ActionMetadataOnly},
		{name: "personal share metered", peer: personal, network: cellular, source: model.SourceShare, kind: model.KindFiles, size: 2 * DefaultRelayMaxSize, want: ActionMetadataOnly},
		{name: "personal explicit", peer: personal, source: model.SourceCLI, kind: model.KindImage, size: 100 * DefaultLANMaxSize, want: ActionAutoApply},
		{name: "offline confirmation", peer: confirmation, network: offline, source: model.SourceClipboard, kind: model.KindText, size: smallSize, want: ActionConfirm},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			network := test.network
			if network == (model.NetworkState{}) {
				network = wifi
			}
			got := Decide(Default(), Input{
				Peer:    test.peer,
				Network: network,
				Path:    model.PathLAN,
				Kind:    test.kind,
				Size:    test.size,
				Source:  test.source,
			})
			if got.Action != test.want {
				t.Fatalf("action: got %s, want %s", got.Action, test.want)
			}
			if got.Eager && got.Action != ActionAutoApply {
				t.Fatalf("%s item is fetched eagerly", got.Action)
			}
		})
	}
}

func TestDecideExplicitOffline(t *testing.T) {
	got := Decide(Default(), Input{
		Peer:    personal,
		Network: offline,
		Path:    model.PathLAN,
		Kind:    model.KindFiles,
		Size:    smallSize,
		Source:  model.SourceCLI,
	})
	if got.Action != ActionAutoApply {
		t.Fatalf("action: got %s, want %s", got.Action, ActionAutoApply)
	}
	if got.Eager {
		t.Fatal("offline item is fetched eagerly")
	}
	if got.Relay {
		t.Fatal("offline item reports the relay as allowed")
	}
}

func TestDecideFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want Action
	}{
		{
			name: "unknown role",
			in:   Input{Peer: Peer{}, Network: wifi, Path: model.PathLAN, Kind: model.KindText, Size: smallSize, Source: model.SourceClipboard},
			want: ActionReject,
		},
		{
			name: "unknown kind",
			in:   Input{Peer: personal, Network: wifi, Path: model.PathLAN, Kind: model.KindUnspecified, Size: smallSize, Source: model.SourceClipboard},
			want: ActionReject,
		},
		{
			name: "unknown source",
			in:   Input{Peer: personal, Network: wifi, Path: model.PathLAN, Kind: model.KindText, Size: smallSize, Source: model.SourceUnspecified},
			want: ActionReject,
		},
		{
			name: "negative size",
			in:   Input{Peer: personal, Network: wifi, Path: model.PathLAN, Kind: model.KindText, Size: -1, Source: model.SourceClipboard},
			want: ActionReject,
		},
		{
			name: "unknown path",
			in:   Input{Peer: personal, Network: wifi, Path: model.PathUnknown, Kind: model.KindText, Size: smallSize, Source: model.SourceClipboard},
			want: ActionMetadataOnly,
		},
		{
			name: "unknown path explicit",
			in:   Input{Peer: personal, Network: wifi, Path: model.PathUnknown, Kind: model.KindText, Size: smallSize, Source: model.SourceCLI},
			want: ActionAutoApply,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Decide(Default(), test.in)
			if got.Action != test.want {
				t.Fatalf("action: got %s, want %s", got.Action, test.want)
			}
			if test.want == ActionReject && (got.Eager || got.Relay) {
				t.Fatalf("rejected item grants eager=%t relay=%t", got.Eager, got.Relay)
			}
		})
	}
}

func TestDecideZeroConfig(t *testing.T) {
	got := Decide(Config{}, Input{
		Peer:    personal,
		Network: wifi,
		Path:    model.PathLAN,
		Kind:    model.KindText,
		Size:    smallSize,
		Source:  model.SourceClipboard,
	})
	if got.Action != ActionReject {
		t.Fatalf("action: got %s, want %s", got.Action, ActionReject)
	}
}

func TestConditionOf(t *testing.T) {
	tests := []struct {
		name  string
		state model.NetworkState
		want  Condition
	}{
		{name: "offline", state: model.NetworkState{}, want: ConditionOffline},
		{name: "wifi", state: wifi, want: ConditionUnmetered},
		{name: "wired", state: model.NetworkState{Online: true}, want: ConditionUnmetered},
		{name: "metered", state: cellular, want: ConditionMetered},
		{name: "roaming", state: roaming, want: ConditionRoaming},
		{name: "data saver", state: saver, want: ConditionDataSaver},
		{name: "data saver wins", state: model.NetworkState{Online: true, Cellular: true, Metered: true, Roaming: true, DataSaver: true}, want: ConditionDataSaver},
		{name: "roaming wins over metered", state: model.NetworkState{Online: true, Cellular: true, Metered: true, Roaming: true}, want: ConditionRoaming},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ConditionOf(test.state); got != test.want {
				t.Fatalf("condition: got %s, want %s", got, test.want)
			}
		})
	}
}

func TestActionStringRoundTrip(t *testing.T) {
	for _, action := range []Action{ActionReject, ActionMetadataOnly, ActionConfirm, ActionAutoApply} {
		parsed, err := ParseAction(action.String())
		if err != nil {
			t.Fatalf("parse %q: %v", action, err)
		}
		if parsed != action {
			t.Fatalf("parse %q: got %s, want %s", action, parsed, action)
		}
	}
	if _, err := ParseAction("apply"); err == nil {
		t.Fatal("parse unknown action: expected an error")
	}
}

func TestConditionString(t *testing.T) {
	tests := map[Condition]string{
		ConditionUnknown:   "unknown",
		ConditionUnmetered: "unmetered",
		ConditionMetered:   "metered",
		ConditionRoaming:   "roaming",
		ConditionDataSaver: "data_saver",
		ConditionOffline:   "offline",
	}
	for condition, want := range tests {
		if got := condition.String(); got != want {
			t.Fatalf("condition %d: got %q, want %q", condition, got, want)
		}
	}
}
