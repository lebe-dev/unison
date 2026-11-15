//go:build !darwin || !cgo

package keystore

import (
	"context"
	"errors"
	"testing"
)

func TestOperationsWithoutCgo(t *testing.T) {
	k, err := New(DefaultService)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	if _, err := k.Load(ctx, "seed"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Load error = %v, want ErrUnsupported", err)
	}
	if err := k.Store(ctx, "seed", []byte("secret")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Store error = %v, want ErrUnsupported", err)
	}
	if err := k.Delete(ctx, "seed"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Delete error = %v, want ErrUnsupported", err)
	}
}
