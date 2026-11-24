package settingsfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/policy"
)

const (
	// Schema is the value of the schema field of every document.
	Schema = "unison-settings"
	// Version is the format version written by this package.
	Version = 1
	// MaxBytes bounds the size of a document. The bound stays below the
	// transport limit of one call, so a file the daemon accepts always fits.
	MaxBytes = 512 << 10
	// maxUnknownKeys bounds the list of keys a document carries beyond the
	// known ones.
	maxUnknownKeys = 32
)

// Document is the content of a settings file. The core settings are known to
// the daemon; the app section carries the local settings of an app and is
// stored without being read.
type Document struct {
	Schema        string                     `json:"schema"`
	FormatVersion int                        `json:"format_version"`
	ExportedAt    string                     `json:"exported_at,omitempty"`
	Platform      string                     `json:"platform,omitempty"`
	AppVersion    string                     `json:"app_version,omitempty"`
	Core          *Core                      `json:"core,omitempty"`
	App           map[string]json.RawMessage `json:"app,omitempty"`
}

// Core is the part of the settings the daemon owns. Every field is optional, so
// an absent field keeps the stored value.
type Core struct {
	// This device asks before it applies a clipboard item.
	RequireConfirmation *bool `json:"require_confirmation,omitempty"`
	// This device asks before it accepts a file transfer.
	RequireFileConfirmation *bool `json:"require_file_confirmation,omitempty"`
	// The paired devices may change the confirmation flags of this device.
	AllowRemoteSettings *bool `json:"allow_remote_settings,omitempty"`
	// Blob store limit in bytes.
	BlobMaxSizeBytes *int64 `json:"blob_max_size_bytes,omitempty"`
	// Text items lose the tracking parameters of their URLs.
	StripTracking *bool `json:"strip_tracking,omitempty"`
	// The app and the daemon may report errors to the developer.
	SentryEnabled *bool `json:"sentry_enabled,omitempty"`
	// The application log records at debug level.
	JournalEnabled *bool `json:"journal_enabled,omitempty"`
	// Lifetimes of the history items.
	HistoryTTL *HistoryTTL `json:"history_ttl,omitempty"`
	// Network policy of the device.
	Policy *Policy `json:"policy,omitempty"`
}

// HistoryTTL is the lifetime of a history item in seconds, per kind.
type HistoryTTL struct {
	Text  *int64 `json:"text,omitempty"`
	Image *int64 `json:"image,omitempty"`
	Files *int64 `json:"files,omitempty"`
}

// Policy is the network policy of the device.
type Policy struct {
	// Largest item fetched eagerly over the relay, in bytes.
	RelayMaxSizeBytes *int64     `json:"relay_max_size_bytes,omitempty"`
	Unmetered         *PolicyRow `json:"unmetered,omitempty"`
	Metered           *PolicyRow `json:"metered,omitempty"`
	Roaming           *PolicyRow `json:"roaming,omitempty"`
	DataSaver         *PolicyRow `json:"data_saver,omitempty"`
}

// PolicyRow is the policy of one network condition.
type PolicyRow struct {
	Text  *PolicyCell `json:"text,omitempty"`
	Image *PolicyCell `json:"image,omitempty"`
	Files *PolicyCell `json:"files,omitempty"`
	// The relay path may be used.
	Relay *bool `json:"relay,omitempty"`
}

// PolicyCell is the outcome for one content kind.
type PolicyCell struct {
	// Outcome for the item: reject, metadata, confirm or auto.
	Action *string `json:"action,omitempty"`
	// Largest item applied without asking, in bytes.
	MaxSizeBytes *int64 `json:"max_size_bytes,omitempty"`
}

// Result describes what Decode found beyond the document itself.
type Result struct {
	// FromNewer means the document was written by a newer app, so only the
	// known keys were applied.
	FromNewer bool
	// Unknown lists the keys of the document this version does not know, in
	// dotted form.
	Unknown []string
}

// Encode renders the document with the current schema and version.
func Encode(doc Document) ([]byte, error) {
	doc.Schema = Schema
	doc.FormatVersion = Version

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("settings file: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Decode reads a document and applies the migrations of the versions it passed.
func Decode(data []byte) (Document, Result, error) {
	return decode(data, Version, migrations)
}

// decode reads a document as a version of target knows it. Tests pass another
// target to exercise the migrations.
func decode(data []byte, target int, steps map[int]func(map[string]any) error) (Document, Result, error) {
	var result Result
	if len(data) == 0 {
		return Document{}, result, errors.New("the settings file is empty")
	}
	if len(data) > MaxBytes {
		return Document{}, result, fmt.Errorf("the settings file is larger than %d bytes", MaxBytes)
	}

	var header struct {
		Schema        string `json:"schema"`
		FormatVersion int    `json:"format_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Document{}, result, fmt.Errorf("the settings file is not valid JSON: %w", err)
	}
	if header.Schema != Schema {
		return Document{}, result, fmt.Errorf("the file is not a Unison settings file: schema %q", header.Schema)
	}

	// A document without a version is taken as one of this version, which
	// keeps a hand written file usable.
	version := header.FormatVersion
	if version == 0 {
		version = target
	}
	if version > target {
		result.FromNewer = true
	}

	raw := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return Document{}, result, fmt.Errorf("the settings file is not valid JSON: %w", err)
	}
	if version <= target {
		collectUnknown(raw, documentNode(), "", &result.Unknown)
		if err := migrate(raw, version, target, steps); err != nil {
			return Document{}, result, err
		}
	}

	normalized, err := json.Marshal(raw)
	if err != nil {
		return Document{}, result, fmt.Errorf("settings file: normalize: %w", err)
	}
	var doc Document
	if err := json.Unmarshal(normalized, &doc); err != nil {
		return Document{}, result, fmt.Errorf("settings file: read: %w", err)
	}
	doc.FormatVersion = version
	if err := doc.Core.validate(); err != nil {
		return Document{}, result, err
	}
	return doc, result, nil
}

// migrate applies the steps from the version of the document to the version of
// the app.
func migrate(doc map[string]any, from, target int, steps map[int]func(map[string]any) error) error {
	for version := from + 1; version <= target; version++ {
		step, ok := steps[version]
		if !ok {
			return fmt.Errorf("settings file: no migration from version %d to %d", version-1, version)
		}
		if err := step(doc); err != nil {
			return fmt.Errorf("settings file: migrate to version %d: %w", version, err)
		}
	}
	return nil
}

// migrations changes a document of the previous version into the current one.
// The document of version 1 is the first format, so it has no steps yet.
var migrations = map[int]func(map[string]any) error{}

// validate reports values the daemon would refuse, so an import fails before
// it changes anything.
func (c *Core) validate() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.BlobMaxSizeBytes != nil && *c.BlobMaxSizeBytes <= 0 {
		errs = append(errs, errors.New("blob_max_size_bytes must be positive"))
	}
	errs = append(errs, c.HistoryTTL.validate()...)
	errs = append(errs, c.Policy.validate()...)
	return errors.Join(errs...)
}

// validate reports lifetimes outside the accepted range.
func (t *HistoryTTL) validate() []error {
	if t == nil {
		return nil
	}
	maxSeconds := int64(history.MaxTTL / time.Second)
	days := int64(history.MaxTTL / (24 * time.Hour))
	fields := []struct {
		name    string
		seconds *int64
	}{
		{"history_ttl.text", t.Text},
		{"history_ttl.image", t.Image},
		{"history_ttl.files", t.Files},
	}
	var errs []error
	for _, field := range fields {
		if field.seconds == nil {
			continue
		}
		switch {
		case *field.seconds <= 0:
			errs = append(errs, fmt.Errorf("%s must be positive", field.name))
		case *field.seconds > maxSeconds:
			errs = append(errs, fmt.Errorf("%s is longer than %d days", field.name, days))
		}
	}
	return errs
}

// validate reports policy values the daemon would refuse.
func (p *Policy) validate() []error {
	if p == nil {
		return nil
	}
	var errs []error
	if p.RelayMaxSizeBytes != nil && *p.RelayMaxSizeBytes <= 0 {
		errs = append(errs, errors.New("policy.relay_max_size_bytes must be positive"))
	}
	rows := []struct {
		name string
		row  *PolicyRow
	}{
		{"unmetered", p.Unmetered},
		{"metered", p.Metered},
		{"roaming", p.Roaming},
		{"data_saver", p.DataSaver},
	}
	for _, item := range rows {
		errs = append(errs, item.row.validate("policy."+item.name)...)
	}
	return errs
}

// validate reports cells the daemon would refuse.
func (r *PolicyRow) validate(prefix string) []error {
	if r == nil {
		return nil
	}
	cells := []struct {
		name string
		cell *PolicyCell
	}{
		{"text", r.Text},
		{"image", r.Image},
		{"files", r.Files},
	}
	var errs []error
	for _, item := range cells {
		if item.cell == nil {
			continue
		}
		path := prefix + "." + item.name
		if item.cell.MaxSizeBytes != nil && *item.cell.MaxSizeBytes < 0 {
			errs = append(errs, fmt.Errorf("%s.max_size_bytes must not be negative", path))
		}
		if item.cell.Action == nil {
			continue
		}
		action, err := policy.ParseAction(*item.cell.Action)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.action: %w", path, err))
			continue
		}
		if action == policy.ActionAutoApply && item.cell.MaxSizeBytes != nil && *item.cell.MaxSizeBytes == 0 {
			errs = append(errs, fmt.Errorf("%s.max_size_bytes must be positive for an automatic apply", path))
		}
	}
	return errs
}

// PlatformName returns the platform name of the document for a Go platform.
func PlatformName(goos string) string {
	if goos == "darwin" {
		return "macos"
	}
	return goos
}

// FileName returns the name of an export file of a platform.
func FileName(platform string, at time.Time) string {
	return fmt.Sprintf("unison-%s-%s.json", platform, at.UTC().Format("20060102-150405"))
}

// node is one level of the known keys of a document.
type node struct {
	// children are the known keys of an object level.
	children map[string]*node
	// any accepts every key of the level.
	any bool
}

// documentNode returns the known keys of a document.
func documentNode() *node {
	cell := &node{children: map[string]*node{
		"action":         {},
		"max_size_bytes": {},
	}}
	row := &node{children: map[string]*node{
		"text":  cell,
		"image": cell,
		"files": cell,
		"relay": {},
	}}
	ttl := &node{children: map[string]*node{
		"text":  {},
		"image": {},
		"files": {},
	}}
	network := &node{children: map[string]*node{
		"relay_max_size_bytes": {},
		"unmetered":            row,
		"metered":              row,
		"roaming":              row,
		"data_saver":           row,
	}}
	core := &node{children: map[string]*node{
		"require_confirmation":      {},
		"require_file_confirmation": {},
		"allow_remote_settings":     {},
		"blob_max_size_bytes":       {},
		"strip_tracking":            {},
		"sentry_enabled":            {},
		"journal_enabled":           {},
		"history_ttl":               ttl,
		"policy":                    network,
	}}
	return &node{children: map[string]*node{
		"schema":         {},
		"format_version": {},
		"exported_at":    {},
		"platform":       {},
		"app_version":    {},
		"core":           core,
		"app":            {any: true},
	}}
}

// collectUnknown appends the keys of value the node does not know.
func collectUnknown(value any, n *node, prefix string, out *[]string) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		child, known := n.children[key]
		if !known {
			if !n.any && len(*out) < maxUnknownKeys {
				*out = append(*out, path)
			}
			continue
		}
		collectUnknown(object[key], child, path, out)
	}
}
