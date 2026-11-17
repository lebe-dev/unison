package trust

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
)

// Settings keys of the group data versions and of the relay access.
const (
	discoveryVersionKey   = "trust.group.discovery_ts"
	discoveryPrevUntilKey = "trust.group.discovery_prev_until"
	relayURLKey           = "trust.group.relay_url"
	relayTokenKey         = "trust.group.relay_token" //nolint:gosec // Settings key, not a credential.
	relayVersionKey       = "trust.group.relay_ts"
)

// DiscoveryTransition is how long the previous discovery key still matches the
// tags of the group after a rotation. The window lets devices that have not
// received the new key yet stay discoverable.
const DiscoveryTransition = time.Hour

// Bounds of the relay settings.
const (
	// MaxRelayURLLength bounds the relay address in bytes.
	MaxRelayURLLength = 2048
	// MaxRelayTokenLength bounds the relay access token in bytes.
	MaxRelayTokenLength = 512
)

// pairRendezvousLabel is the hash prefix of the rendezvous key of a pair that
// never paired directly.
const pairRendezvousLabel = "unison/1 rendezvous pair"

// RelaySettings is the relay access of the Personal group.
type RelaySettings struct {
	// URL is the address of the relay server.
	URL string
	// Token is the access token of the group.
	Token string
}

// Empty reports whether no relay is configured.
func (r RelaySettings) Empty() bool {
	return r.URL == "" && r.Token == ""
}

// validate checks the bounds of the settings.
func (r RelaySettings) validate() error {
	if len(r.URL) > MaxRelayURLLength {
		return fmt.Errorf("%w: relay address is %d bytes", ErrInvalidSecret, len(r.URL))
	}
	if len(r.Token) > MaxRelayTokenLength {
		return fmt.Errorf("%w: relay token is %d bytes", ErrInvalidSecret, len(r.Token))
	}
	return nil
}

// GroupChange reports the parts of the group data that a call replaced.
type GroupChange struct {
	// DiscoveryKey reports a new discovery key.
	DiscoveryKey bool
	// Relay reports new relay settings.
	Relay bool
}

// Any reports whether a part changed.
func (c GroupChange) Any() bool {
	return c.DiscoveryKey || c.Relay
}

// GroupData is the state of the Personal group that its devices share. Every
// part carries the time it last changed, so devices settle on the newest value.
type GroupData struct {
	// DiscoveryKey is the key of the discovery tag.
	DiscoveryKey []byte
	// DiscoveryAt is when the discovery key last changed.
	DiscoveryAt time.Time
	// Relay is the relay access of the group.
	Relay RelaySettings
	// RelayAt is when the relay settings last changed.
	RelayAt time.Time
}

// validate checks the sizes of the group data received from a peer.
func (g GroupData) validate() error {
	if len(g.DiscoveryKey) != 0 && len(g.DiscoveryKey) != secretSize {
		return fmt.Errorf("%w: discovery key is %d bytes", ErrInvalidSecret, len(g.DiscoveryKey))
	}
	return g.Relay.validate()
}

// GroupData returns the group data of the local device.
func (s *Store) GroupData(ctx context.Context) (GroupData, error) {
	settings := s.db.Settings()
	var data GroupData

	discoveryAt, err := settings.GetInt(ctx, discoveryVersionKey, 0)
	if err != nil {
		return GroupData{}, err
	}
	data.DiscoveryAt = timeFromMilli(discoveryAt)
	key, err := s.DiscoveryKey(ctx)
	switch {
	case err == nil:
		data.DiscoveryKey = key
	case errors.Is(err, ErrSecretNotFound):
	default:
		return GroupData{}, err
	}

	relayAt, err := settings.GetInt(ctx, relayVersionKey, 0)
	if err != nil {
		return GroupData{}, err
	}
	data.RelayAt = timeFromMilli(relayAt)
	data.Relay.URL, err = settings.GetString(ctx, relayURLKey, "")
	if err != nil {
		return GroupData{}, err
	}
	data.Relay.Token, err = settings.GetString(ctx, relayTokenKey, "")
	if err != nil {
		return GroupData{}, err
	}
	return data, nil
}

// ApplyGroupData stores the parts of the group data that are newer than the
// local ones and reports what changed.
func (s *Store) ApplyGroupData(ctx context.Context, data GroupData) (GroupChange, error) {
	if err := data.validate(); err != nil {
		return GroupChange{}, err
	}
	limit := time.Now().Add(MaxClockSkew)
	if data.DiscoveryAt.After(limit) || data.RelayAt.After(limit) {
		return GroupChange{}, fmt.Errorf("%w: change time is too far ahead", ErrInvalidSecret)
	}

	var change GroupChange
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		settings := store.NewSettings(tx, s.db.Sealer())

		if len(data.DiscoveryKey) == secretSize {
			stored, err := settings.GetInt(ctx, discoveryVersionKey, 0)
			if err != nil {
				return err
			}
			if unixMilli(data.DiscoveryAt) > stored {
				if err := writeDiscoveryKey(ctx, tx, s.db.Sealer(), data.DiscoveryKey, data.DiscoveryAt); err != nil {
					return err
				}
				change.DiscoveryKey = true
			}
		}

		stored, err := settings.GetInt(ctx, relayVersionKey, 0)
		if err != nil {
			return err
		}
		if data.RelayAt.IsZero() || unixMilli(data.RelayAt) <= stored {
			return nil
		}
		if err := settings.SetString(ctx, relayURLKey, data.Relay.URL); err != nil {
			return err
		}
		if err := settings.SetString(ctx, relayTokenKey, data.Relay.Token); err != nil {
			return err
		}
		if err := settings.SetInt(ctx, relayVersionKey, unixMilli(data.RelayAt)); err != nil {
			return err
		}
		change.Relay = true
		return nil
	})
	if err != nil {
		return GroupChange{}, err
	}
	return change, nil
}

// SetRelaySettings stores the relay access of the group and marks it as changed
// now.
func (s *Store) SetRelaySettings(ctx context.Context, relay RelaySettings) error {
	if err := relay.validate(); err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		settings := store.NewSettings(tx, s.db.Sealer())
		if err := settings.SetString(ctx, relayURLKey, relay.URL); err != nil {
			return err
		}
		if err := settings.SetString(ctx, relayTokenKey, relay.Token); err != nil {
			return err
		}
		return settings.SetInt(ctx, relayVersionKey, unixMilli(time.Now()))
	})
}

// DerivedRendezvousKey returns the relay rendezvous key of a pair that never
// paired directly. Both devices of the pair derive the same value from a secret
// of the group and their device IDs, so no other device has to hand it over,
// and the device IDs alone do not reveal the mailboxes of the pair.
func DerivedRendezvousKey(groupSecret []byte, a, b model.DeviceID) []byte {
	if bytes.Compare(a[:], b[:]) > 0 {
		a, b = b, a
	}
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(groupSecret)))

	hash := blake3.New(secretSize, nil)
	_, _ = hash.Write([]byte(pairRendezvousLabel))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write(groupSecret)
	_, _ = hash.Write(a.Bytes())
	_, _ = hash.Write(b.Bytes())
	return hash.Sum(nil)
}

// RotateDiscoveryKey replaces the discovery key of the group with a new random
// one. The previous key keeps matching the tags for DiscoveryTransition, so the
// devices that have not received the new key yet stay discoverable.
func (s *Store) RotateDiscoveryKey(ctx context.Context) ([]byte, error) {
	key := make([]byte, secretSize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("trust: generate discovery key: %w", err)
	}

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		at, err := nextDiscoveryTime(ctx, tx, s.db.Sealer())
		if err != nil {
			return err
		}
		return writeDiscoveryKey(ctx, tx, s.db.Sealer(), key, at)
	})
	if err != nil {
		return nil, err
	}
	return key, nil
}

// DiscoveryKeys returns the keys that still match the discovery tag of the
// group: the current key and, for DiscoveryTransition after a rotation, the
// previous one. The result is empty when the device has no key.
func (s *Store) DiscoveryKeys(ctx context.Context) ([][]byte, error) {
	keys := make([][]byte, 0, 2)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		sealer := s.db.Sealer()
		current, err := readSecret(ctx, tx, sealer, discoveryKeyName)
		switch {
		case err == nil:
			keys = append(keys, current)
		case errors.Is(err, ErrSecretNotFound):
			return nil
		default:
			return err
		}

		until, err := store.NewSettings(tx, sealer).GetInt(ctx, discoveryPrevUntilKey, 0)
		if err != nil {
			return err
		}
		if time.Now().UnixMilli() >= until {
			return nil
		}
		previous, err := readSecret(ctx, tx, sealer, discoveryPrevKeyName)
		switch {
		case err == nil:
			keys = append(keys, previous)
		case errors.Is(err, ErrSecretNotFound):
		default:
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// LeaveGroup removes the Personal group from the device: the group peers, their
// introductions, the group secrets and the relay access. Friends devices stay
// paired.
func (s *Store) LeaveGroup(ctx context.Context) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peers, err := personalPeerIDs(ctx, tx)
		if err != nil {
			return err
		}
		for _, id := range peers {
			if err := dropRendezvousKey(ctx, tx, id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM peers WHERE role = ?", int(model.RolePersonal)); err != nil {
			return fmt.Errorf("trust: leave the group: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM introductions"); err != nil {
			return fmt.Errorf("trust: leave the group: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM group_secrets WHERE name IN (?, ?)", discoveryKeyName, discoveryPrevKeyName); err != nil {
			return fmt.Errorf("trust: leave the group: %w", err)
		}

		settings := store.NewSettings(tx, s.db.Sealer())
		for _, key := range []string{discoveryVersionKey, discoveryPrevUntilKey, relayURLKey, relayTokenKey, relayVersionKey} {
			if err := settings.Remove(ctx, key); err != nil {
				return err
			}
		}
		return nil
	})
}

// personalPeerIDs returns the device IDs of the Personal peers.
func personalPeerIDs(ctx context.Context, tx *sql.Tx) ([]model.DeviceID, error) {
	rows, err := tx.QueryContext(ctx, "SELECT device_id FROM peers WHERE role = ?", int(model.RolePersonal))
	if err != nil {
		return nil, fmt.Errorf("trust: list personal peers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ids := make([]model.DeviceID, 0, 4)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		id, err := model.DeviceIDFromBytes(raw)
		if err != nil {
			return nil, fmt.Errorf("trust: read peer: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trust: list personal peers: %w", err)
	}
	return ids, nil
}

// writeDiscoveryKey stores the discovery key of the group at at and keeps the
// current one acceptable for the transition period.
func writeDiscoveryKey(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, key []byte, at time.Time) error {
	if len(key) != secretSize {
		return fmt.Errorf("%w: discovery key is %d bytes", ErrInvalidSecret, len(key))
	}
	settings := store.NewSettings(tx, sealer)
	current, err := readSecret(ctx, tx, sealer, discoveryKeyName)
	switch {
	case err == nil:
		if bytes.Equal(current, key) {
			break
		}
		if err := writeSecret(ctx, tx, sealer, discoveryPrevKeyName, current); err != nil {
			return err
		}
		if err := settings.SetInt(ctx, discoveryPrevUntilKey, unixMilli(time.Now().Add(DiscoveryTransition))); err != nil {
			return err
		}
	case errors.Is(err, ErrSecretNotFound):
	default:
		return err
	}

	if err := writeSecret(ctx, tx, sealer, discoveryKeyName, key); err != nil {
		return err
	}
	return settings.SetInt(ctx, discoveryVersionKey, unixMilli(at))
}

// nextDiscoveryTime returns the change time of the next discovery key. It is
// the current time, moved past the stored change so a rotation always wins.
func nextDiscoveryTime(ctx context.Context, tx *sql.Tx, sealer *store.Sealer) (time.Time, error) {
	stored, err := store.NewSettings(tx, sealer).GetInt(ctx, discoveryVersionKey, 0)
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now().UnixMilli()
	if now <= stored {
		now = stored + 1
	}
	return time.UnixMilli(now), nil
}
