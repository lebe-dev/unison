package trust

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

// friendsDiscoveryUntilKey is the settings key of the end of the temporary
// friends discovery mode.
const friendsDiscoveryUntilKey = "trust.friends.discovery_until"

// FriendsDiscoveryUntil returns the end of the local friends discovery mode. It
// is the zero time when the mode is off.
func (s *Store) FriendsDiscoveryUntil(ctx context.Context) (time.Time, error) {
	at, err := s.db.Settings().GetInt(ctx, friendsDiscoveryUntilKey, 0)
	if err != nil {
		return time.Time{}, err
	}
	return timeFromMilli(at), nil
}

// SetFriendsDiscoveryUntil stores the end of the local friends discovery mode. A
// zero time turns the mode off.
func (s *Store) SetFriendsDiscoveryUntil(ctx context.Context, until time.Time) error {
	return s.db.Settings().SetInt(ctx, friendsDiscoveryUntilKey, unixMilli(until))
}

// FriendsDiscoveryKeys returns the rendezvous keys of the Friends devices. The
// keys tag the announcements of the friends discovery mode, so a pair finds
// only each other. Devices without a key are skipped.
func (s *Store) FriendsDiscoveryKeys(ctx context.Context) ([][]byte, error) {
	keys := make([][]byte, 0, 4)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			"SELECT device_id FROM peers WHERE role = ? AND revoked = 0 ORDER BY added_at, device_id",
			int(model.RoleFriends))
		if err != nil {
			return fmt.Errorf("trust: list friends devices: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			id, err := model.DeviceIDFromBytes(raw)
			if err != nil {
				return fmt.Errorf("trust: read peer: %w", err)
			}
			name, err := rendezvousKeyName(id)
			if err != nil {
				return err
			}
			key, err := readSecret(ctx, tx, s.db.Sealer(), name)
			switch {
			case err == nil:
				keys = append(keys, key)
			case errors.Is(err, ErrSecretNotFound):
			default:
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("trust: list friends devices: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}
