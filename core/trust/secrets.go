package trust

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
)

// secretSize is the length of every group secret in bytes.
const secretSize = 32

// Secret names in the database.
const (
	// discoveryKeyName holds the key shared by the Personal group.
	discoveryKeyName = "personal/discovery_key"
	// discoveryPrevKeyName holds the key the group used before the last
	// rotation.
	discoveryPrevKeyName = "personal/discovery_key_prev"
	// rendezvousKeyPrefix addresses the relay rendezvous key of a peer.
	rendezvousKeyPrefix = "rendezvous/"
)

// Settings keys of the confirmation flags of this device for every peer.
const (
	selfRequireConfirmationKey     = "trust.self.require_confirmation"
	selfRequireFileConfirmationKey = "trust.self.require_file_confirmation"
)

// selfConfirmationKey returns the settings key of the flag of the scope.
func selfConfirmationKey(scope Scope) (string, error) {
	switch scope {
	case ScopeClipboard:
		return selfRequireConfirmationKey, nil
	case ScopeFiles:
		return selfRequireFileConfirmationKey, nil
	default:
		return "", fmt.Errorf("%w: unknown confirmation scope %d", ErrInvalidPeer, scope)
	}
}

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

// SetDiscoveryKey stores the key shared by the Personal group, for example the
// one received from another Personal device at pairing. The change is dated, so
// the key travels to the group over gossip.
func (s *Store) SetDiscoveryKey(ctx context.Context, key []byte) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		at, err := nextDiscoveryTime(ctx, tx, s.db.Sealer())
		if err != nil {
			return err
		}
		return writeDiscoveryKey(ctx, tx, s.db.Sealer(), key, at)
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

// SelfRequireConfirmation reports whether this device asks its user before it
// takes the content of the scope from any peer.
func (s *Store) SelfRequireConfirmation(ctx context.Context, scope Scope) (bool, error) {
	key, err := selfConfirmationKey(scope)
	if err != nil {
		return false, err
	}
	return s.db.Settings().GetBool(ctx, key, false)
}

// SelfConfirmations returns the flags of this device for every peer.
func (s *Store) SelfConfirmations(ctx context.Context) (Confirmations, error) {
	clipboard, err := s.SelfRequireConfirmation(ctx, ScopeClipboard)
	if err != nil {
		return Confirmations{}, err
	}
	files, err := s.SelfRequireConfirmation(ctx, ScopeFiles)
	if err != nil {
		return Confirmations{}, err
	}
	return Confirmations{Clipboard: clipboard, Files: files}, nil
}

// SetSelfRequireConfirmation stores whether this device asks its user before it
// takes the content of the scope from any peer.
func (s *Store) SetSelfRequireConfirmation(ctx context.Context, scope Scope, require bool) error {
	key, err := selfConfirmationKey(scope)
	if err != nil {
		return err
	}
	return s.db.Settings().SetBool(ctx, key, require)
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
