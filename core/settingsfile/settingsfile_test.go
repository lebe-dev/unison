package settingsfile

import (
	"encoding/json"
	"testing"
	"time"
)

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// sample returns a document with every field set.
func sample() Document {
	return Document{
		ExportedAt: "2026-11-24T10:20:30Z",
		Platform:   "macos",
		AppVersion: "0.2.0",
		Core: &Core{
			RequireConfirmation:     ptr(true),
			RequireFileConfirmation: ptr(false),
			AllowRemoteSettings:     ptr(true),
			BlobMaxSizeBytes:        ptr(int64(1 << 30)),
			StripTracking:           ptr(true),
			SentryEnabled:           ptr(false),
			JournalEnabled:          ptr(true),
			HistoryTTL: &HistoryTTL{
				Text:  ptr(int64(86400)),
				Image: ptr(int64(172800)),
				Files: ptr(int64(259200)),
			},
			Policy: &Policy{
				RelayMaxSizeBytes: ptr(int64(64 << 20)),
				Unmetered: &PolicyRow{
					Text:  &PolicyCell{Action: ptr("auto"), MaxSizeBytes: ptr(int64(1 << 20))},
					Image: &PolicyCell{Action: ptr("metadata"), MaxSizeBytes: ptr(int64(0))},
					Files: &PolicyCell{Action: ptr("reject"), MaxSizeBytes: ptr(int64(0))},
					Relay: ptr(true),
				},
				Metered: &PolicyRow{
					Text:  &PolicyCell{Action: ptr("confirm")},
					Relay: ptr(false),
				},
			},
		},
		App: map[string]json.RawMessage{
			"desktop": json.RawMessage(`{"theme":"dark","login_item":true}`),
		},
	}
}

// TestEncodeDecodeRoundTrip checks that every field survives the trip.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	data, err := Encode(sample())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	doc, result, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.FromNewer {
		t.Error("the document reports a newer version")
	}
	if len(result.Unknown) != 0 {
		t.Errorf("the document reports unknown keys %v", result.Unknown)
	}
	if doc.Schema != Schema || doc.FormatVersion != Version {
		t.Errorf("schema %q version %d, want %q and %d", doc.Schema, doc.FormatVersion, Schema, Version)
	}
	if doc.Platform != "macos" || doc.AppVersion != "0.2.0" || doc.ExportedAt != "2026-11-24T10:20:30Z" {
		t.Errorf("metadata %q %q %q", doc.Platform, doc.AppVersion, doc.ExportedAt)
	}
	core := doc.Core
	if core == nil {
		t.Fatal("the document has no core settings")
	}
	if *core.RequireConfirmation != true || *core.RequireFileConfirmation != false {
		t.Errorf("confirmation flags %t %t", *core.RequireConfirmation, *core.RequireFileConfirmation)
	}
	if *core.BlobMaxSizeBytes != 1<<30 {
		t.Errorf("blob limit %d", *core.BlobMaxSizeBytes)
	}
	if *core.HistoryTTL.Image != 172800 {
		t.Errorf("image lifetime %d", *core.HistoryTTL.Image)
	}
	if *core.Policy.RelayMaxSizeBytes != 64<<20 {
		t.Errorf("relay limit %d", *core.Policy.RelayMaxSizeBytes)
	}
	if *core.Policy.Unmetered.Files.Action != "reject" {
		t.Errorf("files action %q", *core.Policy.Unmetered.Files.Action)
	}
	if *core.Policy.Metered.Relay != false {
		t.Error("the metered relay flag is not kept")
	}
	if core.Policy.Roaming != nil {
		t.Error("an absent row is reported")
	}
	var app struct {
		Theme     string `json:"theme"`
		LoginItem bool   `json:"login_item"`
	}
	if err := json.Unmarshal(doc.App["desktop"], &app); err != nil {
		t.Fatalf("read the app section: %v", err)
	}
	if app.Theme != "dark" || !app.LoginItem {
		t.Errorf("the app section is %+v", app)
	}
}

// TestEncodeSetsVersion checks that a document always carries the schema and
// the version of this package.
func TestEncodeSetsVersion(t *testing.T) {
	data, err := Encode(Document{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw["schema"] != Schema || raw["format_version"] != float64(Version) {
		t.Errorf("the header is %v", raw)
	}
}

// TestDecodePartial checks that absent keys stay absent, so an import keeps the
// stored values.
func TestDecodePartial(t *testing.T) {
	input := `{
	  "schema": "unison-settings",
	  "format_version": 1,
	  "core": {"strip_tracking": false}
	}`
	doc, _, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if *doc.Core.StripTracking != false {
		t.Error("the present key is not read")
	}
	if doc.Core.RequireConfirmation != nil || doc.Core.BlobMaxSizeBytes != nil {
		t.Error("absent keys are set")
	}
}

// TestDecodeWithoutVersion checks that a hand written document without a
// version is read as one of this version.
func TestDecodeWithoutVersion(t *testing.T) {
	input := `{"schema": "unison-settings", "core": {"sentry_enabled": true}}`
	doc, result, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.FromNewer {
		t.Error("the document reports a newer version")
	}
	if doc.FormatVersion != Version || *doc.Core.SentryEnabled != true {
		t.Errorf("version %d, sentry %t", doc.FormatVersion, *doc.Core.SentryEnabled)
	}
}

// TestDecodeUnknownKeys checks that keys this version does not know are
// reported, and that the app section is not treated as unknown.
func TestDecodeUnknownKeys(t *testing.T) {
	input := `{
	  "schema": "unison-settings",
	  "format_version": 1,
	  "future_flag": true,
	  "core": {
	    "strip_tracking": true,
	    "future_setting": {"a": 1},
	    "policy": {"unmetered": {"text": {"action": "auto", "max_size_bytes": 1, "extra": 2}}}
	  },
	  "app": {"telepathy": {"anything": [1, 2, 3]}}
	}`
	_, result, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{
		"core.future_setting",
		"core.policy.unmetered.text.extra",
		"future_flag",
	}
	if len(result.Unknown) != len(want) {
		t.Fatalf("unknown keys %v, want %v", result.Unknown, want)
	}
	for i, key := range want {
		if result.Unknown[i] != key {
			t.Errorf("unknown key %d is %q, want %q", i, result.Unknown[i], key)
		}
	}
}

// TestDecodeNewerVersion checks that a document of a newer app is read with the
// keys this version knows, and reported.
func TestDecodeNewerVersion(t *testing.T) {
	input := `{
	  "schema": "unison-settings",
	  "format_version": 99,
	  "core": {"blob_max_size_bytes": 2048, "future_setting": 1}
	}`
	doc, result, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.FromNewer {
		t.Error("the document does not report a newer version")
	}
	if len(result.Unknown) != 0 {
		t.Errorf("a newer document reports unknown keys %v", result.Unknown)
	}
	if *doc.Core.BlobMaxSizeBytes != 2048 {
		t.Errorf("blob limit %d, want 2048", *doc.Core.BlobMaxSizeBytes)
	}
}

// TestDecodeMigrations checks that a document of an older version goes through
// every migration of a newer app.
func TestDecodeMigrations(t *testing.T) {
	steps := map[int]func(map[string]any) error{
		2: func(doc map[string]any) error {
			core, _ := doc["core"].(map[string]any)
			core["strip_tracking"] = true
			return nil
		},
		3: func(doc map[string]any) error {
			core, _ := doc["core"].(map[string]any)
			core["journal_enabled"] = false
			return nil
		},
	}
	input := `{"schema": "unison-settings", "format_version": 1, "core": {"blob_max_size_bytes": 1}}`
	doc, result, err := decode([]byte(input), 3, steps)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.FromNewer {
		t.Error("the document reports a newer version")
	}
	if *doc.Core.StripTracking != true || *doc.Core.JournalEnabled != false {
		t.Errorf("the migrations did not run: %+v", doc.Core)
	}
	if doc.FormatVersion != 1 {
		t.Errorf("the document version is %d, want its own 1", doc.FormatVersion)
	}
}

// TestDecodeMissingMigration checks that a gap in the migrations is an error
// rather than a silent half conversion.
func TestDecodeMissingMigration(t *testing.T) {
	input := `{"schema": "unison-settings", "format_version": 1}`
	if _, _, err := decode([]byte(input), 2, map[int]func(map[string]any) error{}); err == nil {
		t.Fatal("a missing migration is accepted")
	}
}

// TestDecodeRejectsWrongFile checks the errors of a file that is not a
// settings document.
func TestDecodeRejectsWrongFile(t *testing.T) {
	cases := map[string]string{
		"empty":        ``,
		"not json":     `{`,
		"wrong schema": `{"schema": "other", "format_version": 1}`,
		"no schema":    `{"format_version": 1}`,
		"array":        `[]`,
	}
	for name, input := range cases {
		if _, _, err := Decode([]byte(input)); err == nil {
			t.Errorf("%s: the document is accepted", name)
		}
	}
}

// TestDecodeTooLarge checks the size bound.
func TestDecodeTooLarge(t *testing.T) {
	data := make([]byte, MaxBytes+1)
	for i := range data {
		data[i] = ' '
	}
	if _, _, err := Decode(data); err == nil {
		t.Fatal("an oversized document is accepted")
	}
}

// TestDecodeValidation checks that values the daemon would refuse fail before
// anything is applied.
func TestDecodeValidation(t *testing.T) {
	cases := map[string]string{
		"blob limit":     `{"core": {"blob_max_size_bytes": 0}}`,
		"ttl zero":       `{"core": {"history_ttl": {"text": 0}}}`,
		"ttl too long":   `{"core": {"history_ttl": {"files": 100000000}}}`,
		"relay limit":    `{"core": {"policy": {"relay_max_size_bytes": -1}}}`,
		"action":         `{"core": {"policy": {"metered": {"text": {"action": "maybe"}}}}}`,
		"auto no limit":  `{"core": {"policy": {"metered": {"text": {"action": "auto", "max_size_bytes": 0}}}}}`,
		"negative limit": `{"core": {"policy": {"metered": {"image": {"max_size_bytes": -5}}}}}`,
	}
	for name, body := range cases {
		input := `{"schema": "unison-settings", "format_version": 1, ` + body[1:]
		if _, _, err := Decode([]byte(input)); err == nil {
			t.Errorf("%s: the document is accepted", name)
		}
	}
}

// TestDecodeRejectsBrokenTypes checks that a value of the wrong JSON type is an
// error.
func TestDecodeRejectsBrokenTypes(t *testing.T) {
	input := `{"schema": "unison-settings", "format_version": 1, "core": {"strip_tracking": "yes"}}`
	if _, _, err := Decode([]byte(input)); err == nil {
		t.Fatal("a string in a flag is accepted")
	}
}

// TestFileName checks the name of an export file.
func TestFileName(t *testing.T) {
	at := time.Date(2026, 11, 24, 10, 20, 30, 0, time.UTC)
	if got := FileName("macos", at); got != "unison-macos-20261124-102030.json" {
		t.Errorf("the file name is %q", got)
	}
	other := time.FixedZone("plus3", 3*60*60)
	if got := FileName("android", at.In(other)); got != "unison-android-20261124-102030.json" {
		t.Errorf("the name of another zone is %q", got)
	}
}

// TestPlatformName checks the platform names of the document.
func TestPlatformName(t *testing.T) {
	cases := map[string]string{
		"darwin":  "macos",
		"windows": "windows",
		"linux":   "linux",
		"android": "android",
	}
	for goos, want := range cases {
		if got := PlatformName(goos); got != want {
			t.Errorf("platform of %s is %q, want %q", goos, got, want)
		}
	}
}
