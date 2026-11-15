package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// settingsTable is the table holding typed settings in one sealed value per key.
const settingsTable = "settings"

// Querier is implemented by *sql.DB and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Settings reads and writes settings. Values are stored sealed, so only their
// keys are visible in the database file.
type Settings struct {
	q      Querier
	sealer *Sealer
}

// NewSettings creates a settings handle over q. A domain that keeps settings in
// its own transaction passes that transaction.
func NewSettings(q Querier, sealer *Sealer) *Settings {
	return &Settings{q: q, sealer: sealer}
}

// SetString stores a string setting.
func (s *Settings) SetString(ctx context.Context, key, value string) error {
	return s.set(ctx, key, value)
}

// SetInt stores an integer setting.
func (s *Settings) SetInt(ctx context.Context, key string, value int64) error {
	return s.set(ctx, key, strconv.FormatInt(value, 10))
}

// SetBool stores a boolean setting.
func (s *Settings) SetBool(ctx context.Context, key string, value bool) error {
	return s.set(ctx, key, strconv.FormatBool(value))
}

// SetDuration stores a duration setting.
func (s *Settings) SetDuration(ctx context.Context, key string, value time.Duration) error {
	return s.set(ctx, key, value.String())
}

// GetString returns the string setting stored under key, or def when the key is
// absent.
func (s *Settings) GetString(ctx context.Context, key, def string) (string, error) {
	value, ok, err := s.get(ctx, key)
	if err != nil || !ok {
		return def, err
	}
	return value, nil
}

// GetInt returns the integer setting stored under key, or def when the key is
// absent.
func (s *Settings) GetInt(ctx context.Context, key string, def int64) (int64, error) {
	value, ok, err := s.get(ctx, key)
	if err != nil || !ok {
		return def, err
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return def, fmt.Errorf("store: setting %q is not an integer: %w", key, err)
	}
	return parsed, nil
}

// GetBool returns the boolean setting stored under key, or def when the key is
// absent.
func (s *Settings) GetBool(ctx context.Context, key string, def bool) (bool, error) {
	value, ok, err := s.get(ctx, key)
	if err != nil || !ok {
		return def, err
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return def, fmt.Errorf("store: setting %q is not a boolean: %w", key, err)
	}
	return parsed, nil
}

// GetDuration returns the duration setting stored under key, or def when the
// key is absent.
func (s *Settings) GetDuration(ctx context.Context, key string, def time.Duration) (time.Duration, error) {
	value, ok, err := s.get(ctx, key)
	if err != nil || !ok {
		return def, err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return def, fmt.Errorf("store: setting %q is not a duration: %w", key, err)
	}
	return parsed, nil
}

// Remove deletes a setting.
func (s *Settings) Remove(ctx context.Context, key string) error {
	if _, err := s.q.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", key); err != nil {
		return fmt.Errorf("store: remove setting %q: %w", key, err)
	}
	return nil
}

// set seals value and writes it under key.
func (s *Settings) set(ctx context.Context, key, value string) error {
	if key == "" {
		return errors.New("store: setting key is required")
	}
	sealed, err := s.sealer.Seal(settingsTable, key, []byte(value))
	if err != nil {
		return fmt.Errorf("store: seal setting %q: %w", key, err)
	}

	const query = `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
	if _, err := s.q.ExecContext(ctx, query, key, sealed, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("store: write setting %q: %w", key, err)
	}
	return nil
}

// get returns the sealed value of key and whether it exists.
func (s *Settings) get(ctx context.Context, key string) (string, bool, error) {
	var sealed []byte
	err := s.q.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: read setting %q: %w", key, err)
	}

	value, err := s.sealer.Open(settingsTable, key, sealed)
	if err != nil {
		return "", false, fmt.Errorf("store: open setting %q: %w", key, err)
	}
	return string(value), true, nil
}
