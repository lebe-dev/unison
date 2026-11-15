package policy

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
)

// fakeSources builds the peer, network and path sources of a decider.
type fakeSources struct {
	peer    Peer
	peerErr error
	network model.NetworkState
	path    model.Path
}

func (f *fakeSources) peerFunc(context.Context, model.DeviceID) (Peer, error) {
	return f.peer, f.peerErr
}

func newTestDecider(t *testing.T, src *fakeSources) *Decider {
	t.Helper()
	decider, err := New(Options{
		Peer:    src.peerFunc,
		Network: func(context.Context) model.NetworkState { return src.network },
		Path:    func(context.Context, model.DeviceID) model.Path { return src.path },
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return decider
}

func TestNewRequiresSources(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("new without sources: expected an error")
	}
	if _, err := New(Options{Peer: func(context.Context, model.DeviceID) (Peer, error) { return Peer{}, nil }}); err == nil {
		t.Fatal("new without a network source: expected an error")
	}
}

func TestDeciderDecision(t *testing.T) {
	item := model.ClipItem{
		Kind:            model.KindImage,
		Representations: []model.Representation{{MIME: "image/png", Size: smallSize}},
	}
	var from model.DeviceID

	tests := []struct {
		name    string
		src     fakeSources
		item    model.ClipItem
		config  *Config
		want    clip.Decision
		wantErr bool
	}{
		{name: "personal applies", src: fakeSources{peer: personal, network: wifi, path: model.PathLAN}, item: item, want: clip.DecisionApply},
		{
			name: "confirmation asks",
			src:  fakeSources{peer: Peer{Role: model.RolePersonal, RequireConfirmation: true}, network: wifi, path: model.PathLAN},
			item: item,
			want: clip.DecisionSkip,
		},
		{
			name: "files kept as metadata",
			src:  fakeSources{peer: personal, network: wifi, path: model.PathLAN},
			item: model.ClipItem{Kind: model.KindFiles, Representations: []model.Representation{{Size: smallSize}}},
			want: clip.DecisionSkip,
		},
		{
			name:    "peer error",
			src:     fakeSources{peerErr: errors.New("no peer"), network: wifi, path: model.PathLAN},
			item:    item,
			want:    clip.DecisionSkip,
			wantErr: true,
		},
		{
			name: "config override",
			src:  fakeSources{peer: personal, network: wifi, path: model.PathLAN},
			item: item,
			config: func() *Config {
				cfg := Default()
				cfg.Unmetered.Image = Cell{Action: ActionConfirm, MaxSize: megabyte}
				return &cfg
			}(),
			want: clip.DecisionSkip,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decider := newTestDecider(t, &test.src)
			if test.config != nil {
				decider.SetConfig(*test.config)
			}
			got, err := decider.Decide(context.Background(), from, test.item)
			if test.wantErr != (err != nil) {
				t.Fatalf("error: got %v, want error %t", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("decision: got %s, want %s", got, test.want)
			}
		})
	}
}

func TestDeciderDecideItem(t *testing.T) {
	item := model.ClipItem{
		Kind:            model.KindFiles,
		Representations: []model.Representation{{Size: smallSize}},
	}
	src := fakeSources{peer: personal, network: wifi, path: model.PathLAN}
	decider := newTestDecider(t, &src)

	result, err := decider.DecideItem(context.Background(), model.DeviceID{}, item, model.SourceShare)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if result.Action != ActionAutoApply || !result.Eager {
		t.Fatalf("share: got %s eager=%t, want %s", result.Action, result.Eager, ActionAutoApply)
	}

	result, err = decider.DecideItem(context.Background(), model.DeviceID{}, item, model.SourceClipboard)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if result.Action != ActionMetadataOnly {
		t.Fatalf("announcement: got %s, want %s", result.Action, ActionMetadataOnly)
	}
}

func TestDeciderSetConfigCopies(t *testing.T) {
	src := fakeSources{peer: personal, network: wifi, path: model.PathLAN}
	decider := newTestDecider(t, &src)

	cfg := Default()
	decider.SetConfig(cfg)
	cfg.Metered.Image = Cell{Action: ActionReject}

	if got := decider.Config(); got.Metered.Image.Action != ActionAutoApply {
		t.Fatalf("config changed through the argument: got %s", got.Metered.Image.Action)
	}
	if decider.Config() != Default() {
		t.Fatal("config: got a modified value, want the defaults")
	}
}

func TestDeciderDefaults(t *testing.T) {
	src := fakeSources{peer: personal, network: wifi, path: model.PathLAN}
	decider := newTestDecider(t, &src)
	if decider.Config() != Default() {
		t.Fatal("new decider does not start with the default configuration")
	}
}

func TestItemSize(t *testing.T) {
	tests := []struct {
		name string
		item model.ClipItem
		want int64
	}{
		{name: "empty", item: model.ClipItem{}, want: 0},
		{
			name: "sum",
			item: model.ClipItem{Representations: []model.Representation{{Size: 3}, {Size: 4}}},
			want: 7,
		},
		{
			name: "inline",
			item: model.ClipItem{Representations: []model.Representation{{Inline: []byte("hello")}}},
			want: 5,
		},
		{
			name: "saturates",
			item: model.ClipItem{Representations: []model.Representation{
				{Size: math.MaxInt64},
				{Size: math.MaxInt64},
			}},
			want: math.MaxInt64,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ItemSize(test.item); got != test.want {
				t.Fatalf("size: got %d, want %d", got, test.want)
			}
		})
	}
}
