package trust

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
)

// Settings keys of the group data versions and of the relay access.
const (
	discoveryVersionKey = "trust.group.discovery_ts"
	relayURLKey         = "trust.group.relay_url"
	relayTokenKey       = "trust.group.relay_token"
	relayVersionKey     = "trust.group.relay_ts"
)

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
// local ones and reports whether the discovery key changed.
func (s *Store) ApplyGroupData(ctx context.Context, data GroupData) (bool, error) {
	if err := data.validate(); err != nil {
		return false, err
	}
	limit := time.Now().Add(MaxClockSkew)
	if data.DiscoveryAt.After(limit) || data.RelayAt.After(limit) {
		return false, fmt.Errorf("%w: change time is too far ahead", ErrInvalidSecret)
	}

	changed := false
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		settings := store.NewSettings(tx, s.db.Sealer())

		if len(data.DiscoveryKey) == secretSize {
			stored, err := settings.GetInt(ctx, discoveryVersionKey, 0)
			if err != nil {
				return err
			}
			if unixMilli(data.DiscoveryAt) > stored {
				if err := writeSecret(ctx, tx, s.db.Sealer(), discoveryKeyName, data.DiscoveryKey); err != nil {
					return err
				}
				if err := settings.SetInt(ctx, discoveryVersionKey, unixMilli(data.DiscoveryAt)); err != nil {
					return err
				}
				changed = true
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
		return settings.SetInt(ctx, relayVersionKey, unixMilli(data.RelayAt))
	})
	if err != nil {
		return false, err
	}
	return changed, nil
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
// paired directly. Both devices of the pair derive the same value, so no other
// device has to hand it over.
func DerivedRendezvousKey(a, b model.DeviceID) []byte {
	if bytes.Compare(a[:], b[:]) > 0 {
		a, b = b, a
	}
	hash := blake3.New(secretSize, nil)
	_, _ = hash.Write([]byte(pairRendezvousLabel))
	_, _ = hash.Write(a.Bytes())
	_, _ = hash.Write(b.Bytes())
	return hash.Sum(nil)
}
