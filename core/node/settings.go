package node

import (
	"context"
	"fmt"

	"github.com/lebe-dev/unison/core/store"
)

// SettingSentryEnabled is the settings key of the consent to report errors.
const SettingSentryEnabled = "sentry.enabled"

// SettingStripTracking is the settings key of the removal of the advertising
// parameters of the URLs in text items.
const SettingStripTracking = "clipboard.strip_tracking"

// DefaultStripTracking is whether a device removes the advertising parameters
// of the URLs in text items before it stores and announces them.
const DefaultStripTracking = true

// SentryEnabled returns the stored consent to report errors. Before the node
// starts the store is opened for the lookup, so a startup failure is reported
// for a user who already granted the consent.
func (n *Node) SentryEnabled(ctx context.Context) (bool, error) {
	db := n.db
	if db == nil {
		migrations, err := migrationSet()
		if err != nil {
			return false, fmt.Errorf("node: read the sentry consent: %w", err)
		}
		db, err = store.Open(ctx, store.Options{
			Dir:        n.cfg.DataDir,
			Keystore:   n.cfg.Keystore,
			Migrations: migrations,
		})
		if err != nil {
			return false, fmt.Errorf("node: read the sentry consent: %w", err)
		}
		defer func() { _ = db.Close() }()
	}

	enabled, err := db.Settings().GetBool(ctx, SettingSentryEnabled, false)
	if err != nil {
		return false, fmt.Errorf("node: read the sentry consent: %w", err)
	}
	return enabled, nil
}

// StripTracking reports whether the URLs in text items lose their advertising
// parameters.
func (n *Node) StripTracking(ctx context.Context) (bool, error) {
	enabled, err := n.db.Settings().GetBool(ctx, SettingStripTracking, DefaultStripTracking)
	if err != nil {
		return false, fmt.Errorf("node: read the tracking setting: %w", err)
	}
	return enabled, nil
}

// SetStripTracking stores the removal of the advertising parameters of the
// URLs in text items. The next captured and applied item uses the new value.
func (n *Node) SetStripTracking(ctx context.Context, enabled bool) error {
	if err := n.db.Settings().SetBool(ctx, SettingStripTracking, enabled); err != nil {
		return fmt.Errorf("node: store the tracking setting: %w", err)
	}
	if n.clip != nil {
		n.clip.SetStripTracking(enabled)
	}
	n.log.Info("URL tracking parameters setting changed", "enabled", enabled)
	return nil
}
