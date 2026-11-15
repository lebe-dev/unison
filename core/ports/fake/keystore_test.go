package fake

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/lebe-dev/unison/core/ports"
)

func TestKeystore(t *testing.T) {
	ctx := context.Background()
	keystore := NewKeystore()

	if _, err := keystore.Load(ctx, "seed"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("load missing secret: got %v, want ErrNotFound", err)
	}

	secret := []byte{1, 2, 3}
	if err := keystore.Store(ctx, "seed", secret); err != nil {
		t.Fatalf("store: %v", err)
	}
	secret[0] = 9

	got, err := keystore.Load(ctx, "seed")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := []byte{1, 2, 3}; !slices.Equal(got, want) {
		t.Fatalf("load: got %v, want %v", got, want)
	}

	got[1] = 9
	again, err := keystore.Load(ctx, "seed")
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if want := []byte{1, 2, 3}; !slices.Equal(again, want) {
		t.Fatalf("load again: got %v, want %v", again, want)
	}

	if err := keystore.Delete(ctx, "seed"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := keystore.Delete(ctx, "seed"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	if _, err := keystore.Load(ctx, "seed"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("load after delete: got %v, want ErrNotFound", err)
	}
}

func TestKeystoreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	keystore := NewKeystore()
	if err := keystore.Store(ctx, "seed", []byte{1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("store: got %v, want context.Canceled", err)
	}
	if _, err := keystore.Load(ctx, "seed"); !errors.Is(err, context.Canceled) {
		t.Fatalf("load: got %v, want context.Canceled", err)
	}
}
