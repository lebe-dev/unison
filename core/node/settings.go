package node

import (
	"context"
	"fmt"

	"github.com/lebe-dev/unison/core/store"
)

// SettingSentryEnabled is the settings key of the consent to report errors.
const SettingSentryEnabled = "sentry.enabled"

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
