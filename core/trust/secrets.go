package trust

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
)

// secretSize is the length of every group secret in bytes.
const secretSize = 32

// Secret names in the database.
const (
	// discoveryKeyName holds the key shared by the Personal group.
	discoveryKeyName = "personal/discovery_key"
	// rendezvousKeyPrefix addresses the relay rendezvous key of a peer.
	rendezvousKeyPrefix = "rendezvous/"
)

// selfRequireConfirmationKey is the settings key of the local confirmation
// policy advertised to peers in the handshake.
const selfRequireConfirmationKey = "trust.self.require_confirmation"

// DiscoveryKey returns the key shared by the Personal group.
func (s *Store) DiscoveryKey(ctx context.Context) ([]byte, error) {
	var key []byte
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		key, err = readSecret(ctx, tx, s.db.Sealer(), discoveryKeyName)
		return err
	})
	if err != nil {
		return nil, err
	}
	return key, nil
}

// EnsureDiscoveryKey returns the key shared by the Personal group, generating
// and storing it when the group has none yet.
func (s *Store) EnsureDiscoveryKey(ctx context.Context) ([]byte, error) {
	var key []byte
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		existing, err := readSecret(ctx, tx, s.db.Sealer(), discoveryKeyName)
		switch {
		case err == nil:
			key = existing
			return nil
		case !errors.Is(err, ErrSecretNotFound):
			return err
		}

		key = make([]byte, secretSize)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("trust: generate discovery key: %w", err)
		}
		if err := writeSecret(ctx, tx, s.db.Sealer(), discoveryKeyName, key); err != nil {
			return err
		}
		return store.NewSettings(tx, s.db.Sealer()).SetInt(ctx, discoveryVersionKey, unixMilli(time.Now()))
	})
	if err != nil {
		return nil, err
	}
	return key, nil
}

// SetDiscoveryKey stores the key shared by the Personal group, for example the
// one received from another Personal device at pairing. The change is marked
// with the current time, so the key travels to the group over gossip.
func (s *Store) SetDiscoveryKey(ctx context.Context, key []byte) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := writeSecret(ctx, tx, s.db.Sealer(), discoveryKeyName, key); err != nil {
			return err
		}
		return store.NewSettings(tx, s.db.Sealer()).SetInt(ctx, discoveryVersionKey, unixMilli(time.Now()))
	})
}

// RendezvousKey returns the relay rendezvous key shared with a peer.
func (s *Store) RendezvousKey(ctx context.Context, id model.DeviceID) ([]byte, error) {
	name, err := rendezvousKeyName(id)
	if err != nil {
		return nil, err
	}
	var key []byte
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		key, err = readSecret(ctx, tx, s.db.Sealer(), name)
		return err
	})
	if err != nil {
		return nil, err
	}
	return key, nil
}

// SetRendezvousKey stores the relay rendezvous key shared with a peer.
func (s *Store) SetRendezvousKey(ctx context.Context, id model.DeviceID, key []byte) error {
	name, err := rendezvousKeyName(id)
	if err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		return writeSecret(ctx, tx, s.db.Sealer(), name, key)
	})
}

// SelfRequireConfirmation reports whether the local device asks peers to
// confirm clipboard items before applying them.
func (s *Store) SelfRequireConfirmation(ctx context.Context) (bool, error) {
	return s.db.Settings().GetBool(ctx, selfRequireConfirmationKey, false)
}

// SetSelfRequireConfirmation stores the local confirmation policy advertised to
// peers in the handshake.
func (s *Store) SetSelfRequireConfirmation(ctx context.Context, require bool) error {
	return s.db.Settings().SetBool(ctx, selfRequireConfirmationKey, require)
}

// rendezvousKeyName returns the secret name of the rendezvous key of a peer.
func rendezvousKeyName(id model.DeviceID) (string, error) {
	if id.IsZero() {
		return "", fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	return rendezvousKeyPrefix + id.String(), nil
}

// readSecret opens the sealed secret stored under name.
func readSecret(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, name string) ([]byte, error) {
	var sealed []byte
	err := tx.QueryRowContext(ctx, "SELECT value FROM group_secrets WHERE name = ?", name).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecretNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("trust: read secret: %w", err)
	}

	key, err := sealer.Open(secretsTable, name, sealed)
	if err != nil {
		return nil, fmt.Errorf("trust: open secret: %w", err)
	}
	if len(key) != secretSize {
		return nil, fmt.Errorf("%w: stored secret is %d bytes, want %d", ErrInvalidSecret, len(key), secretSize)
	}
	return key, nil
}

// writeSecret seals key and stores it under name.
func writeSecret(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, name string, key []byte) error {
	if len(key) != secretSize {
		return fmt.Errorf("%w: secret is %d bytes, want %d", ErrInvalidSecret, len(key), secretSize)
	}
	sealed, err := sealer.Seal(secretsTable, name, key)
	if err != nil {
		return fmt.Errorf("trust: seal secret: %w", err)
	}

	const query = `INSERT INTO group_secrets (name, value) VALUES (?, ?)
		ON CONFLICT (name) DO UPDATE SET value = excluded.value`
	if _, err := tx.ExecContext(ctx, query, name, sealed); err != nil {
		return fmt.Errorf("trust: write secret: %w", err)
	}
	return nil
}
