package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"
)

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})
	settings := s.Settings()

	if got, err := settings.GetString(ctx, "missing", "def"); err != nil || got != "def" {
		t.Fatalf("default string: got %q, %v, want def", got, err)
	}
	if got, err := settings.GetInt(ctx, "missing", 42); err != nil || got != 42 {
		t.Fatalf("default int: got %d, %v, want 42", got, err)
	}
	if got, err := settings.GetBool(ctx, "missing", true); err != nil || !got {
		t.Fatalf("default bool: got %t, %v, want true", got, err)
	}
	if got, err := settings.GetDuration(ctx, "missing", time.Minute); err != nil || got != time.Minute {
		t.Fatalf("default duration: got %v, %v, want 1m", got, err)
	}

	writes := []struct {
		name  string
		write func(key string) error
		read  func(key string) (any, error)
		want  any
	}{
		{
			name:  "string",
			write: func(key string) error { return settings.SetString(ctx, key, "dark") },
			read: func(key string) (any, error) {
				value, err := settings.GetString(ctx, key, "")
				return value, err
			},
			want: "dark",
		},
		{
			name:  "empty string",
			write: func(key string) error { return settings.SetString(ctx, key, "") },
			read: func(key string) (any, error) {
				value, err := settings.GetString(ctx, key, "def")
				return value, err
			},
			want: "",
		},
		{
			name:  "int",
			write: func(key string) error { return settings.SetInt(ctx, key, -7) },
			read: func(key string) (any, error) {
				value, err := settings.GetInt(ctx, key, 0)
				return value, err
			},
			want: int64(-7),
		},
		{
			name:  "bool",
			write: func(key string) error { return settings.SetBool(ctx, key, false) },
			read: func(key string) (any, error) {
				value, err := settings.GetBool(ctx, key, true)
				return value, err
			},
			want: false,
		},
		{
			name:  "duration",
			write: func(key string) error { return settings.SetDuration(ctx, key, 36*time.Hour) },
			read: func(key string) (any, error) {
				value, err := settings.GetDuration(ctx, key, 0)
				return value, err
			},
			want: 36 * time.Hour,
		},
	}

	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			key := "test." + write.name
			if err := write.write(key); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := write.read(key)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if got != write.want {
				t.Fatalf("read: got %v, want %v", got, write.want)
			}
		})
	}

	if err := settings.SetString(ctx, "test.string", "light"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got, err := settings.GetString(ctx, "test.string", ""); err != nil || got != "light" {
		t.Fatalf("read overwritten: got %q, %v, want light", got, err)
	}

	if err := settings.Remove(ctx, "test.string"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got, err := settings.GetString(ctx, "test.string", "def"); err != nil || got != "def" {
		t.Fatalf("read removed: got %q, %v, want def", got, err)
	}
	if err := settings.Remove(ctx, "test.string"); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestSettingsRejectBadValues(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})
	settings := s.Settings()

	if err := settings.SetString(ctx, "number", "many"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := settings.GetInt(ctx, "number", 0); err == nil {
		t.Fatal("read int from a word: want an error")
	}

	if err := settings.SetString(ctx, "flag", "maybe"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := settings.GetBool(ctx, "flag", false); err == nil {
		t.Fatal("read bool from a word: want an error")
	}

	if err := settings.SetString(ctx, "ttl", "week"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := settings.GetDuration(ctx, "ttl", 0); err == nil {
		t.Fatal("read duration from a word: want an error")
	}

	if err := settings.SetString(ctx, "", "value"); err == nil {
		t.Fatal("write with an empty key: want an error")
	}
}

func TestSettingsRejectForeignValue(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})

	sealed, err := s.Sealer().Seal("settings", "key", []byte("value"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		const query = "INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)"
		_, err := tx.ExecContext(ctx, query, "other", sealed, 0)
		return err
	}); err != nil {
		t.Fatalf("insert relocated value: %v", err)
	}

	if _, err := s.Settings().GetString(ctx, "other", ""); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("read relocated value: got %v, want ErrDecrypt", err)
	}
}

func TestSettingsInsideDomainTransaction(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		settings := NewSettings(tx, s.Sealer())
		return settings.SetInt(ctx, "peers.max", 12)
	}); err != nil {
		t.Fatalf("write in transaction: %v", err)
	}

	if got, err := s.Settings().GetInt(ctx, "peers.max", 0); err != nil || got != 12 {
		t.Fatalf("read committed setting: got %d, %v, want 12", got, err)
	}

	wantErr := errors.New("stop")
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		settings := NewSettings(tx, s.Sealer())
		if err := settings.SetInt(ctx, "peers.max", 99); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("rollback: got %v, want %v", err, wantErr)
	}
	if got, err := s.Settings().GetInt(ctx, "peers.max", 0); err != nil || got != 12 {
		t.Fatalf("read after rollback: got %d, %v, want 12", got, err)
	}
}

func TestSettingsAreSealedOnDisk(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, Options{})

	const secret = "text-that-must-not-be-on-disk"
	if err := s.Settings().SetString(ctx, "history.text", secret); err != nil {
		t.Fatalf("set: %v", err)
	}

	var raw []byte
	if err := s.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", "history.text").Scan(&raw)
	}); err != nil {
		t.Fatalf("read raw value: %v", err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("stored value contains the plaintext")
	}

	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	for _, file := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("%s contains the plaintext", file)
		}
	}
}
