package api_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// TestGetPauseOff checks the pause state of a running pipeline.
func TestGetPause(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1000)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one", Clock: n.clock})

	ctx := callTimeout(t)
	response, err := client.GetPause(ctx, connect.NewRequest(&localv1.GetPauseRequest{}))
	if err != nil {
		t.Fatalf("get pause: %v", err)
	}
	if response.Msg.GetPause().GetPaused() {
		t.Error("the clipboard is paused")
	}
}

// TestSetPauseIndefinite checks the pause without a deadline.
func TestSetPauseIndefinite(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1100)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one", Clock: n.clock})
	ctx := callTimeout(t)

	response, err := client.SetPause(ctx, connect.NewRequest(&localv1.SetPauseRequest{
		Mode: localv1.PauseMode_PAUSE_MODE_INDEFINITE,
	}))
	if err != nil {
		t.Fatalf("set pause: %v", err)
	}
	pause := response.Msg.GetPause()
	if !pause.GetPaused() || !pause.GetIndefinite() || pause.GetUntilUnixMs() != 0 {
		t.Fatalf("pause is %v, want an indefinite pause", pause)
	}

	stored, err := client.GetPause(ctx, connect.NewRequest(&localv1.GetPauseRequest{}))
	if err != nil {
		t.Fatalf("get pause: %v", err)
	}
	if !stored.Msg.GetPause().GetIndefinite() {
		t.Fatalf("stored pause is %v, want an indefinite pause", stored.Msg.GetPause())
	}
}

// TestSetPauseUntil checks the pause with a deadline and its expiry.
func TestSetPauseUntil(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1200)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one", Clock: n.clock})
	ctx := callTimeout(t)

	until := n.clock.Now().Add(time.Hour)
	response, err := client.SetPause(ctx, connect.NewRequest(&localv1.SetPauseRequest{
		Mode:        localv1.PauseMode_PAUSE_MODE_UNTIL,
		UntilUnixMs: until.UnixMilli(),
	}))
	if err != nil {
		t.Fatalf("set pause: %v", err)
	}
	pause := response.Msg.GetPause()
	if !pause.GetPaused() || pause.GetIndefinite() {
		t.Fatalf("pause is %v, want a pause with a deadline", pause)
	}
	if pause.GetUntilUnixMs() != until.UnixMilli() {
		t.Errorf("pause deadline is %d, want %d", pause.GetUntilUnixMs(), until.UnixMilli())
	}

	n.clock.Advance(2 * time.Hour)
	expired, err := client.GetPause(ctx, connect.NewRequest(&localv1.GetPauseRequest{}))
	if err != nil {
		t.Fatalf("get pause: %v", err)
	}
	if expired.Msg.GetPause().GetPaused() {
		t.Fatalf("pause is %v, want no pause after the deadline", expired.Msg.GetPause())
	}
}

// TestSetPauseOff checks that an indefinite pause ends.
func TestSetPauseOff(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1300)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one", Clock: n.clock})
	ctx := callTimeout(t)

	if _, err := client.SetPause(ctx, connect.NewRequest(&localv1.SetPauseRequest{
		Mode: localv1.PauseMode_PAUSE_MODE_INDEFINITE,
	})); err != nil {
		t.Fatalf("set pause: %v", err)
	}

	response, err := client.SetPause(ctx, connect.NewRequest(&localv1.SetPauseRequest{
		Mode: localv1.PauseMode_PAUSE_MODE_OFF,
	}))
	if err != nil {
		t.Fatalf("set pause off: %v", err)
	}
	if response.Msg.GetPause().GetPaused() {
		t.Fatalf("pause is %v, want no pause", response.Msg.GetPause())
	}
}

// TestSetPauseInvalid checks the refused pause requests.
func TestSetPauseInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1400)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one", Clock: n.clock})
	ctx := callTimeout(t)

	_, err := client.SetPause(ctx, connect.NewRequest(&localv1.SetPauseRequest{}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = client.SetPause(ctx, connect.NewRequest(&localv1.SetPauseRequest{
		Mode:        localv1.PauseMode_PAUSE_MODE_UNTIL,
		UntilUnixMs: n.clock.Now().Add(-time.Hour).UnixMilli(),
	}))
	requireCode(t, err, connect.CodeInvalidArgument)
}

// TestPauseNotRunning checks that a stopped node refuses the pause calls.
func TestPauseNotRunning(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 1500)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	_, err := client.GetPause(ctx, connect.NewRequest(&localv1.GetPauseRequest{}))
	requireCode(t, err, connect.CodeFailedPrecondition)
}
