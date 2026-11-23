package policy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/lebe-dev/unison/core/model"
)

// fakeSettings is an in-memory Settings for the tests.
type fakeSettings map[string]string

func (f fakeSettings) GetString(_ context.Context, key, def string) (string, error) {
	if value, ok := f[key]; ok {
		return value, nil
	}
	return def, nil
}

func (f fakeSettings) GetInt(_ context.Context, key string, def int64) (int64, error) {
	value, ok := f[key]
	if !ok {
		return def, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return def, fmt.Errorf("setting %q is not an integer: %w", key, err)
	}
	return parsed, nil
}

func (f fakeSettings) GetBool(_ context.Context, key string, def bool) (bool, error) {
	value, ok := f[key]
	if !ok {
		return def, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return def, fmt.Errorf("setting %q is not a boolean: %w", key, err)
	}
	return parsed, nil
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(context.Background(), fakeSettings{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg != Default() {
		t.Fatalf("config without settings: got %+v, want the defaults", cfg)
	}
}

// TestLoadConfigCells covers the override of every cell of every row.
func TestLoadConfigCells(t *testing.T) {
	for _, condition := range Conditions() {
		for _, kind := range Kinds() {
			for _, action := range []Action{ActionReject, ActionMetadataOnly, ActionConfirm} {
				t.Run(condition.String()+"/"+kind.String()+"/"+action.String(), func(t *testing.T) {
					settings := fakeSettings{ModeKey(condition, kind): action.String()}
					cfg, err := LoadConfig(context.Background(), settings)
					if err != nil {
						t.Fatalf("load: %v", err)
					}
					row, _ := cfg.Row(condition)
					cell, _ := row.Cell(kind)
					if cell.Action != action {
						t.Fatalf("action: got %s, want %s", cell.Action, action)
					}

					got := Decide(cfg, Input{
						Peer:    personal,
						Network: networkOf(condition),
						Path:    model.PathLAN,
						Kind:    kind,
						Size:    smallSize,
						Source:  model.SourceClipboard,
					})
					if got.Action != action {
						t.Fatalf("decision: got %s, want %s", got.Action, action)
					}
				})
			}
		}
	}
}

func TestLoadConfigSizeOverrides(t *testing.T) {
	settings := fakeSettings{
		MaxSizeKey(ConditionUnmetered, model.KindImage): strconv.Itoa(20 * megabyte),
		MaxSizeKey(ConditionUnmetered, model.KindText):  strconv.Itoa(2 * megabyte),
	}
	cfg, err := LoadConfig(context.Background(), settings)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	tests := []struct {
		kind model.Kind
		size int64
		want Action
	}{
		{kind: model.KindImage, size: 15 * megabyte, want: ActionAutoApply},
		{kind: model.KindImage, size: 20*megabyte + 1, want: ActionMetadataOnly},
		{kind: model.KindText, size: 2 * megabyte, want: ActionAutoApply},
		{kind: model.KindText, size: 2*megabyte + 1, want: ActionMetadataOnly},
		{kind: model.KindImage, size: DefaultLANMaxSize, want: ActionAutoApply},
	}

	for _, test := range tests {
		got := Decide(cfg, Input{
			Peer:    personal,
			Network: wifi,
			Path:    model.PathLAN,
			Kind:    test.kind,
			Size:    test.size,
			Source:  model.SourceClipboard,
		})
		if got.Action != test.want {
			t.Fatalf("%s at %d bytes: got %s, want %s", test.kind, test.size, got.Action, test.want)
		}
	}
}

func TestLoadConfigRelayOverrides(t *testing.T) {
	settings := fakeSettings{
		RelayKey(ConditionUnmetered):               "false",
		RelayMaxSizeKey:                            strconv.Itoa(2 * megabyte),
		ModeKey(ConditionMetered, model.KindImage): "metadata",
		RelayFilesKey:                              "false",
	}
	cfg, err := LoadConfig(context.Background(), settings)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Unmetered.Relay {
		t.Fatal("relay is still allowed on an unmetered network")
	}
	if !cfg.Metered.Relay {
		t.Fatal("relay was disabled on a metered network")
	}
	if cfg.RelayMaxSize != 2*megabyte {
		t.Fatalf("relay limit: got %d, want %d", cfg.RelayMaxSize, 2*megabyte)
	}
	if cfg.RelayFilesOnRequest {
		t.Fatal("the file rule did not change")
	}

	viaRelay := Decide(cfg, Input{
		Peer:    personal,
		Network: wifi,
		Path:    model.PathRelay,
		Kind:    model.KindImage,
		Size:    smallSize,
		Source:  model.SourceClipboard,
	})
	if viaRelay.Action != ActionReject {
		t.Fatalf("relay on a restricted network: got %s, want %s", viaRelay.Action, ActionReject)
	}

	metered := Decide(cfg, Input{
		Peer:    personal,
		Network: cellular,
		Path:    model.PathLAN,
		Kind:    model.KindImage,
		Size:    smallSize,
		Source:  model.SourceClipboard,
	})
	if metered.Action != ActionMetadataOnly {
		t.Fatalf("metered image: got %s, want %s", metered.Action, ActionMetadataOnly)
	}
}

func TestLoadConfigInvalidMode(t *testing.T) {
	settings := fakeSettings{ModeKey(ConditionUnmetered, model.KindImage): "always"}
	cfg, err := LoadConfig(context.Background(), settings)
	if err == nil {
		t.Fatal("load: expected an error")
	}
	if cfg != Default() {
		t.Fatalf("invalid mode: got %+v, want the defaults", cfg)
	}
}

func TestLoadConfigInvalidSize(t *testing.T) {
	settings := fakeSettings{MaxSizeKey(ConditionMetered, model.KindImage): "-1"}
	cfg, err := LoadConfig(context.Background(), settings)
	if err == nil {
		t.Fatal("load: expected an error")
	}
	row, _ := cfg.Row(ConditionMetered)
	cell, _ := row.Cell(model.KindImage)
	if cell.MaxSize != DefaultRelayMaxSize {
		t.Fatalf("invalid size: got %d, want the default %d", cell.MaxSize, DefaultRelayMaxSize)
	}
}

func TestLoadConfigInvalidRelayLimit(t *testing.T) {
	settings := fakeSettings{RelayMaxSizeKey: "0"}
	cfg, err := LoadConfig(context.Background(), settings)
	if err == nil {
		t.Fatal("load: expected an error")
	}
	if cfg.RelayMaxSize != DefaultRelayMaxSize {
		t.Fatalf("relay limit: got %d, want the default %d", cfg.RelayMaxSize, DefaultRelayMaxSize)
	}
}

func TestLoadConfigSettingsError(t *testing.T) {
	want := errors.New("storage failed")
	settings := failingSettings{err: want}
	if _, err := LoadConfig(context.Background(), settings); !errors.Is(err, want) {
		t.Fatalf("load: got %v, want %v", err, want)
	}
}

// failingSettings reports every read as an error.
type failingSettings struct {
	err error
}

func (f failingSettings) GetString(context.Context, string, string) (string, error) {
	return "", f.err
}

func (f failingSettings) GetInt(context.Context, string, int64) (int64, error) {
	return 0, f.err
}

func (f failingSettings) GetBool(context.Context, string, bool) (bool, error) {
	return false, f.err
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{name: "default", mutate: func(*Config) {}, want: true},
		{
			name: "auto without limit",
			mutate: func(cfg *Config) {
				cfg.Unmetered.Image = Cell{Action: ActionAutoApply}
			},
			want: false,
		},
		{
			name: "unknown action",
			mutate: func(cfg *Config) {
				cfg.Metered.Text = Cell{Action: ActionAutoApply + 1, MaxSize: megabyte}
			},
			want: false,
		},
		{
			name: "negative limit",
			mutate: func(cfg *Config) {
				cfg.Roaming.Files = Cell{Action: ActionMetadataOnly, MaxSize: -1}
			},
			want: false,
		},
		{
			name: "relay limit",
			mutate: func(cfg *Config) {
				cfg.RelayMaxSize = 0
			},
			want: false,
		},
		{
			name: "metadata without limit",
			mutate: func(cfg *Config) {
				cfg.DataSaver.Image = Cell{Action: ActionMetadataOnly}
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Default()
			test.mutate(&cfg)
			err := cfg.Validate()
			if test.want && err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !test.want && err == nil {
				t.Fatal("validate: expected an error")
			}
		})
	}
}

func TestKeys(t *testing.T) {
	keys := Keys()
	want := len(Conditions())*(2*len(Kinds())+1) + 2
	if len(keys) != want {
		t.Fatalf("keys: got %d, want %d", len(keys), want)
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if seen[key] {
			t.Fatalf("duplicate key %q", key)
		}
		seen[key] = true
	}
	for _, want := range []string{
		"policy.network.unmetered.image.mode",
		"policy.network.unmetered.image.max_size",
		"policy.network.data_saver.relay",
		RelayMaxSizeKey,
		RelayFilesKey,
	} {
		if !seen[want] {
			t.Fatalf("missing key %q", want)
		}
	}
}

// networkOf returns a network state that selects condition.
func networkOf(condition Condition) model.NetworkState {
	switch condition {
	case ConditionUnmetered:
		return wifi
	case ConditionMetered:
		return cellular
	case ConditionRoaming:
		return roaming
	case ConditionDataSaver:
		return saver
	default:
		return offline
	}
}
