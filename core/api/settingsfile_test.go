package api_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/settingsfile"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/internal/journal"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// exportSettings returns the document of a started client.
func exportSettings(t *testing.T, client localv1connect.UnisonServiceClient, app string) *localv1.ExportSettingsResponse {
	t.Helper()

	ctx := callTimeout(t)
	response, err := client.ExportSettings(ctx, connect.NewRequest(&localv1.ExportSettingsRequest{AppSettings: app}))
	if err != nil {
		t.Fatalf("export settings: %v", err)
	}
	return response.Msg
}

// TestExportSettings checks the document of a configured device.
func TestExportSettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2300)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one", Clock: n.clock, Version: "9.9.9"})
	ctx := callTimeout(t)

	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		RequireConfirmation:     ptr(true),
		RequireFileConfirmation: ptr(true),
		BlobMaxSizeBytes:        ptr(int64(1 << 20)),
		StripTracking:           ptr(false),
		HistoryTtlTextSeconds:   ptr(int64(3600)),
		Metered: &localv1.PolicyRow{
			Text: &localv1.PolicyCell{
				Action:       ptr(localv1.PolicyAction_POLICY_ACTION_AUTO),
				MaxSizeBytes: ptr(int64(4096)),
			},
		},
	})); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	exported := exportSettings(t, client, `{"desktop": {"theme": "dark"}}`)
	platform := settingsfile.PlatformName(runtime.GOOS)
	if want := settingsfile.FileName(platform, n.clock.Now()); exported.GetFileName() != want {
		t.Errorf("file name %q, want %q", exported.GetFileName(), want)
	}

	doc, result, err := settingsfile.Decode(exported.GetDocument())
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	if result.FromNewer || len(result.Unknown) != 0 {
		t.Errorf("the document reports %+v", result)
	}
	if doc.Platform != platform || doc.AppVersion != "9.9.9" {
		t.Errorf("platform %q, version %q", doc.Platform, doc.AppVersion)
	}
	if want := n.clock.Now().UTC().Format(time.RFC3339); doc.ExportedAt != want {
		t.Errorf("exported at %q, want %q", doc.ExportedAt, want)
	}

	core := doc.Core
	if !*core.RequireConfirmation || !*core.RequireFileConfirmation {
		t.Error("the confirmation flags are lost")
	}
	if *core.BlobMaxSizeBytes != 1<<20 {
		t.Errorf("blob limit %d", *core.BlobMaxSizeBytes)
	}
	if *core.StripTracking {
		t.Error("the strip tracking flag is lost")
	}
	if *core.HistoryTTL.Text != 3600 {
		t.Errorf("text lifetime %d", *core.HistoryTTL.Text)
	}
	cell := core.Policy.Metered.Text
	if *cell.Action != "auto" || *cell.MaxSizeBytes != 4096 {
		t.Errorf("the metered text cell is %+v", cell)
	}
	if core.Policy.Unmetered == nil || core.Policy.Roaming == nil || core.Policy.DataSaver == nil {
		t.Error("a policy row is absent")
	}

	if string(doc.App["desktop"]) != `{"theme":"dark"}` {
		t.Errorf("the app section is %s", doc.App["desktop"])
	}
}

// TestExportSettingsRejectsAppSection checks that the local settings of the app
// have to be an object.
func TestExportSettingsRejectsAppSection(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2310)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	for _, app := range []string{"[]", `"text"`, "[]"} {
		_, err := client.ExportSettings(ctx, connect.NewRequest(&localv1.ExportSettingsRequest{AppSettings: app}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("the app section %q is accepted: %v", app, err)
		}
	}
}

// TestImportSettings checks that a document restores the settings it carries.
func TestImportSettings(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2320)
	n.start(t)
	appLog, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open the journal: %v", err)
	}
	t.Cleanup(func() { _ = appLog.Close() })
	client := newClient(t, n.Node, api.Options{DeviceName: "one", Journal: appLog})
	ctx := callTimeout(t)

	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		RequireConfirmation:    ptr(true),
		BlobMaxSizeBytes:       ptr(int64(1 << 20)),
		StripTracking:          ptr(false),
		HistoryTtlImageSeconds: ptr(int64(7200)),
	})); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	exported := exportSettings(t, client, "")

	// Move every setting away from the document, then restore it.
	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		RequireConfirmation:    ptr(false),
		BlobMaxSizeBytes:       ptr(int64(2 << 20)),
		StripTracking:          ptr(true),
		HistoryTtlImageSeconds: ptr(int64(60)),
	})); err != nil {
		t.Fatalf("change settings: %v", err)
	}

	imported, err := client.ImportSettings(ctx, connect.NewRequest(&localv1.ImportSettingsRequest{
		Document: exported.GetDocument(),
	}))
	if err != nil {
		t.Fatalf("import settings: %v", err)
	}
	settings := imported.Msg.GetSettings()
	if !settings.GetRequireConfirmation() {
		t.Error("the confirmation flag is not restored")
	}
	if settings.GetBlobMaxSizeBytes() != 1<<20 {
		t.Errorf("blob limit %d", settings.GetBlobMaxSizeBytes())
	}
	if settings.GetStripTracking() {
		t.Error("the strip tracking flag is not restored")
	}
	if settings.GetHistoryTtlImageSeconds() != 7200 {
		t.Errorf("image lifetime %d", settings.GetHistoryTtlImageSeconds())
	}
	if imported.Msg.GetFromNewerVersion() || len(imported.Msg.GetIgnored()) != 0 {
		t.Errorf("the import reports %v %v", imported.Msg.GetFromNewerVersion(), imported.Msg.GetIgnored())
	}
}

// TestImportSettingsPartial checks that keys absent from the document keep
// their stored values.
func TestImportSettingsPartial(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2330)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	if _, err := client.UpdateSettings(ctx, connect.NewRequest(&localv1.UpdateSettingsRequest{
		RequireConfirmation: ptr(true),
	})); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	document := `{"schema": "unison-settings", "format_version": 1, "core": {"strip_tracking": false}}`
	imported, err := client.ImportSettings(ctx, connect.NewRequest(&localv1.ImportSettingsRequest{
		Document: []byte(document),
	}))
	if err != nil {
		t.Fatalf("import settings: %v", err)
	}
	settings := imported.Msg.GetSettings()
	if settings.GetStripTracking() {
		t.Error("the present key is not applied")
	}
	if !settings.GetRequireConfirmation() {
		t.Error("an absent key is changed")
	}
}

// TestImportSettingsNewer checks that a document of a newer app applies the
// known keys and says so.
func TestImportSettingsNewer(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2340)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	document := `{"schema": "unison-settings", "format_version": 99, "core": {"blob_max_size_bytes": 4096, "future": 1}}`
	imported, err := client.ImportSettings(ctx, connect.NewRequest(&localv1.ImportSettingsRequest{
		Document: []byte(document),
	}))
	if err != nil {
		t.Fatalf("import settings: %v", err)
	}
	if !imported.Msg.GetFromNewerVersion() {
		t.Error("the import does not report a newer version")
	}
	if imported.Msg.GetSettings().GetBlobMaxSizeBytes() != 4096 {
		t.Errorf("blob limit %d", imported.Msg.GetSettings().GetBlobMaxSizeBytes())
	}
	if len(imported.Msg.GetIgnored()) != 0 {
		t.Errorf("a newer document reports ignored keys %v", imported.Msg.GetIgnored())
	}
}

// TestImportSettingsUnknownKeys checks that keys of the same version this
// version does not know are reported, including a device name a hand written
// file may carry.
func TestImportSettingsUnknownKeys(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2350)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	document := `{
	  "schema": "unison-settings",
	  "format_version": 1,
	  "future_flag": true,
	  "core": {"device_name": "other", "future_setting": 1}
	}`
	imported, err := client.ImportSettings(ctx, connect.NewRequest(&localv1.ImportSettingsRequest{
		Document: []byte(document),
	}))
	if err != nil {
		t.Fatalf("import settings: %v", err)
	}
	ignored := strings.Join(imported.Msg.GetIgnored(), ",")
	if ignored != "core.device_name,core.future_setting,future_flag" {
		t.Errorf("ignored keys %q", ignored)
	}
	if imported.Msg.GetSettings().GetDeviceName() != "one" {
		t.Errorf("the device name is %q", imported.Msg.GetSettings().GetDeviceName())
	}
}

// TestImportSettingsRejectsBrokenDocument checks that a file that is not a
// settings document is refused.
func TestImportSettingsRejectsBrokenDocument(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2360)
	n.start(t)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	for _, document := range []string{``, `{`, `{"schema": "other"}`, `{"schema": "unison-settings", "core": {"blob_max_size_bytes": 0}}`} {
		_, err := client.ImportSettings(ctx, connect.NewRequest(&localv1.ImportSettingsRequest{
			Document: []byte(document),
		}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("the document %q is accepted: %v", document, err)
		}
	}
}

// TestImportSettingsNotRunning checks that the import needs a started daemon.
func TestImportSettingsNotRunning(t *testing.T) {
	n := newNode(t, vnet.NewNetwork(), fake.NewBus(), 2370)
	client := newClient(t, n.Node, api.Options{DeviceName: "one"})
	ctx := callTimeout(t)

	document := `{"schema": "unison-settings", "format_version": 1}`
	_, err := client.ImportSettings(ctx, connect.NewRequest(&localv1.ImportSettingsRequest{
		Document: []byte(document),
	}))
	requireCode(t, err, connect.CodeFailedPrecondition)
}
