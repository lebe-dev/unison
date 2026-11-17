package api_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/internal/observability"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// ptr returns a pointer to v.
func ptr[T any](v T) *T {
	return &v
}

// TestGetSettings checks the settings of a started node.
func TestGetSettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2100)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})

	ctx := callTimeout(t)
	response, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	settings := response.Msg.GetSettings()
	if settings.GetDeviceName() != "one" {
		t.Errorf("device name is %q, want %q", settings.GetDeviceName(), "one")
	}
	if settings.GetRequireConfirmation() {
		t.Error("the device asks for confirmation")
	}
	if limit := settings.GetBlobMaxSizeBytes(); limit != blob.DefaultLimitDesktop {
		t.Errorf("blob limit is %d, want %d", limit, int64(blob.DefaultLimitDesktop))
	}
}

// TestUpdateSettings checks that the stored settings are reported back.
func TestUpdateSettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2200)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		DeviceName:              ptr("two"),
		RequireConfirmation:     ptr(true),
		RequireFileConfirmation: ptr(true),
		BlobMaxSizeBytes:        ptr(int64(1 << 20)),
	}))
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	settings := response.Msg.GetSettings()
	if settings.GetDeviceName() != "two" {
		t.Errorf("device name is %q, want %q", settings.GetDeviceName(), "two")
	}
	if !settings.GetRequireConfirmation() {
		t.Error("the device does not ask for confirmation")
	}
	if !settings.GetRequireFileConfirmation() {
		t.Error("the device does not ask before it accepts files")
	}
	if settings.GetBlobMaxSizeBytes() != 1<<20 {
		t.Errorf("blob limit is %d, want %d", settings.GetBlobMaxSizeBytes(), int64(1<<20))
	}

	stored, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if stored.Msg.GetSettings().GetDeviceName() != "two" {
		t.Errorf("stored device name is %q, want %q", stored.Msg.GetSettings().GetDeviceName(), "two")
	}

	// A later call changes one setting only.
	partial, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		BlobMaxSizeBytes: ptr(int64(2 << 20)),
	}))
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if partial.Msg.GetSettings().GetDeviceName() != "two" {
		t.Errorf("device name is %q, want %q", partial.Msg.GetSettings().GetDeviceName(), "two")
	}
	if partial.Msg.GetSettings().GetBlobMaxSizeBytes() != 2<<20 {
		t.Errorf("blob limit is %d, want %d", partial.Msg.GetSettings().GetBlobMaxSizeBytes(), int64(2<<20))
	}
}

// TestUpdateSettingsInvalid checks the refused settings.
func TestUpdateSettingsInvalid(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2300)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	cases := map[string]*localv1.UpdateSettingsRequest{
		"empty name":    {DeviceName: ptr("   ")},
		"long name":     {DeviceName: ptr(strings.Repeat("n", 129))},
		"zero size":     {BlobMaxSizeBytes: ptr(int64(0))},
		"negative size": {BlobMaxSizeBytes: ptr(int64(-1))},
	}
	for name, request := range cases {
		_, err := client.UpdateSettings(ctx, connect.NewRequest(request))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: error is %v, want invalid argument", name, err)
		}
	}
}

// TestUpdateSettingsSentryConsent checks that the consent to report errors is
// stored and gates the reporting.
func TestUpdateSettingsSentryConsent(t *testing.T) {
	observability.SetEnabled(false)
	t.Cleanup(func() { observability.SetEnabled(false) })

	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2500)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	response, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		SentryEnabled: ptr(true),
	}))
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if !response.Msg.GetSettings().GetSentryEnabled() {
		t.Error("the consent is not reported")
	}
	if !observability.Enabled() {
		t.Error("the consent does not gate the reporting")
	}

	stored, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if !stored.Msg.GetSettings().GetSentryEnabled() {
		t.Error("the stored consent is not reported")
	}

	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		SentryEnabled: ptr(false),
	})); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if observability.Enabled() {
		t.Error("the withdrawn consent still gates the reporting")
	}

	// A service created later applies the stored value before the first call.
	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		SentryEnabled: ptr(true),
	})); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	observability.SetEnabled(false)
	_ = newClient(t, n.Node, api.Options{DeviceName: "one"})
	if !observability.Enabled() {
		t.Error("the stored consent is not applied at startup")
	}
}

// TestUpdateSettingsNotRunning checks that a stopped node refuses the settings.
func TestUpdateSettingsNotRunning(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2400)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	_, err := client.GetSettings(ctx, connect.NewRequest(&localv1.GetSettingsRequest{}))
	requireCode(t, err, connect.CodeFailedPrecondition)
}
