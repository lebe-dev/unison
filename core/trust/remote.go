package trust

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lebe-dev/unison/core/model"
)

// allowRemoteSettingsKey is the settings key of the permission for every peer.
const allowRemoteSettingsKey = "trust.allow_remote_settings"

// AllowRemoteSettings reports whether every paired device may change the
// confirmation flags of this device remotely. The permission is granted until
// the user turns it off.
func (s *Store) AllowRemoteSettings(ctx context.Context) (bool, error) {
	return s.db.Settings().GetBool(ctx, allowRemoteSettingsKey, true)
}

// SetAllowRemoteSettings stores the permission for every paired device.
func (s *Store) SetAllowRemoteSettings(ctx context.Context, allow bool) error {
	return s.db.Settings().SetBool(ctx, allowRemoteSettingsKey, allow)
}

// SetPeerAllowRemoteSettings stores the permission of one paired device to
// change the confirmation flags of this device remotely. It reports whether the
// permission changed.
func (s *Store) SetPeerAllowRemoteSettings(ctx context.Context, id model.DeviceID, allow bool) (bool, error) {
	denied := !allow
	changed := false
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := s.activePeer(ctx, tx, id)
		if err != nil {
			return err
		}
		if peer.RemoteSettingsDenied == denied {
			return nil
		}
		peer.RemoteSettingsDenied = denied
		changed = true
		return s.writeProfile(ctx, tx, peer)
	})
	if err != nil {
		return false, fmt.Errorf("trust: set the remote settings permission: %w", err)
	}
	return changed, nil
}
