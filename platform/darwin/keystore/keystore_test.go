package keystore

import (
	"context"
	"strings"
	"testing"
)

func TestNewRejectsEmptyService(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("New(\"\") returned no error")
	}
}

func TestNewKeepsService(t *testing.T) {
	k, err := New(DefaultService)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := k.Service(); got != DefaultService {
		t.Fatalf("Service() = %q, want %q", got, DefaultService)
	}
}

func TestErrorIncludesStatus(t *testing.T) {
	err := &Error{Op: "load", Status: -25300}
	msg := err.Error()
	if !strings.Contains(msg, "load") || !strings.Contains(msg, "-25300") {
		t.Fatalf("Error() = %q", msg)
	}
}

func TestCanceledContextFailsFast(t *testing.T) {
	k, err := New(DefaultService)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := k.Load(ctx, "canceled"); err == nil {
		t.Fatal("Load with canceled context returned no error")
	}
	if err := k.Store(ctx, "canceled", []byte("secret")); err == nil {
		t.Fatal("Store with canceled context returned no error")
	}
	if err := k.Delete(ctx, "canceled"); err == nil {
		t.Fatal("Delete with canceled context returned no error")
	}
}
