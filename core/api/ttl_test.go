package api_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// secondsOf returns a lifetime in whole seconds.
func secondsOf(d time.Duration) int64 {
	return int64(d / time.Second)
}

// TestGetSettingsHistoryTTL checks that the daemon reports the lifetimes it
// captures items with.
func TestGetSettingsHistoryTTL(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2900)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})

	ctx := callTimeout(t)
	response, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	settings := response.Msg.GetSettings()
	if got := settings.GetHistoryTtlTextSeconds(); got != secondsOf(history.DefaultTextTTL) {
		t.Errorf("text lifetime is %d, want %d", got, secondsOf(history.DefaultTextTTL))
	}
	if got := settings.GetHistoryTtlImageSeconds(); got != secondsOf(history.DefaultImageTTL) {
		t.Errorf("image lifetime is %d, want %d", got, secondsOf(history.DefaultImageTTL))
	}
	if got := settings.GetHistoryTtlFilesSeconds(); got != secondsOf(history.DefaultFilesTTL) {
		t.Errorf("file lifetime is %d, want %d", got, secondsOf(history.DefaultFilesTTL))
	}
}

// TestUpdateHistoryTTLRoundTrip checks that the lifetimes survive the API and
// that a partial change keeps the other kinds.
func TestUpdateHistoryTTLRoundTrip(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3000)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	text := secondsOf(30 * 24 * time.Hour)
	image := secondsOf(12 * time.Hour)
	response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		HistoryTtlTextSeconds:  &text,
		HistoryTtlImageSeconds: &image,
	}))
	if err != nil {
		t.Fatalf("update the lifetimes: %v", err)
	}

	settings := response.Msg.GetSettings()
	if got := settings.GetHistoryTtlTextSeconds(); got != text {
		t.Errorf("text lifetime is %d, want %d", got, text)
	}
	if got := settings.GetHistoryTtlImageSeconds(); got != image {
		t.Errorf("image lifetime is %d, want %d", got, image)
	}
	if got := settings.GetHistoryTtlFilesSeconds(); got != secondsOf(history.DefaultFilesTTL) {
		t.Errorf("file lifetime is %d, want the default %d", got, secondsOf(history.DefaultFilesTTL))
	}

	stored, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if got := stored.Msg.GetSettings().GetHistoryTtlTextSeconds(); got != text {
		t.Errorf("stored text lifetime is %d, want %d", got, text)
	}
}

// TestUpdateHistoryTTLReset checks that the reset drops the stored lifetimes.
func TestUpdateHistoryTTLReset(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3100)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	text := secondsOf(30 * 24 * time.Hour)
	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		HistoryTtlTextSeconds: &text,
	})); err != nil {
		t.Fatalf("update the lifetime: %v", err)
	}

	response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		ResetHistoryTtl: true,
	}))
	if err != nil {
		t.Fatalf("reset the lifetimes: %v", err)
	}
	if got := response.Msg.GetSettings().GetHistoryTtlTextSeconds(); got != secondsOf(history.DefaultTextTTL) {
		t.Errorf("text lifetime is %d after the reset, want %d", got, secondsOf(history.DefaultTextTTL))
	}

	stored, err := n.Store().Settings().GetString(ctx, history.SettingTextTTL, "")
	if err != nil {
		t.Fatalf("read the stored lifetime: %v", err)
	}
	if stored != "" {
		t.Errorf("the stored lifetime is %q after the reset", stored)
	}
}

// TestUpdateHistoryTTLInvalid checks the refused lifetimes.
func TestUpdateHistoryTTLInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3200)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	zero := int64(0)
	negative := int64(-1)
	tooLong := secondsOf(history.MaxTTL + time.Hour)
	cases := map[string]*localv1.UpdateSettingsRequest{
		"zero text":      {HistoryTtlTextSeconds: &zero},
		"negative image": {HistoryTtlImageSeconds: &negative},
		"long files":     {HistoryTtlFilesSeconds: &tooLong},
	}
	for name, request := range cases {
		_, err := client.UpdateSettings(ctx, connect.NewRequest(request))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: error is %v, want invalid argument", name, err)
		}
	}

	settings, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if got := settings.Msg.GetSettings().GetHistoryTtlTextSeconds(); got != secondsOf(history.DefaultTextTTL) {
		t.Errorf("text lifetime is %d after the refused changes, want the default", got)
	}
}

// TestUpdateHistoryTTLAppliesWithoutRestart checks that the next captured item
// gets the new lifetime.
func TestUpdateHistoryTTLAppliesWithoutRestart(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 3300)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	before := captureText(t, n, "before")
	if got := before.ExpiresAt.Sub(before.CreatedAt); got != history.DefaultTextTTL {
		t.Fatalf("the lifetime before the change is %s, want %s", got, history.DefaultTextTTL)
	}

	lifetime := 90 * time.Minute
	seconds := secondsOf(lifetime)
	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		HistoryTtlTextSeconds: &seconds,
	})); err != nil {
		t.Fatalf("update the lifetime: %v", err)
	}

	after := captureText(t, n, "after")
	if got := after.ExpiresAt.Sub(after.CreatedAt); got != lifetime {
		t.Errorf("the lifetime after the change is %s, want %s", got, lifetime)
	}
}

// captureText copies text into the clipboard of the node and returns the item
// the pipeline records for it.
func captureText(t *testing.T, n *testNode, text string) model.ClipItem {
	t.Helper()

	var previous model.ClipID
	if item, ok := n.Clip().Last(); ok {
		previous = item.ID
	}
	n.clipboard.Emit(ports.Snapshot{Content: ports.Content{
		Representations: []ports.Representation{{MIME: clip.MIMETextPlain, Data: []byte(text)}},
	}})

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if item, ok := n.Clip().Last(); ok && item.ID != previous {
			return item
		}
		n.clock.Advance(clip.DefaultDebounce)
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the pipeline did not capture %q", text)
	return model.ClipItem{}
}
