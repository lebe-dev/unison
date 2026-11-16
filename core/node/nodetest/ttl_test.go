package nodetest

import (
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/history"
)

// TestHistoryTTLChange checks that a saved lifetime applies to the next item and
// that a short lifetime removes the item from the history.
func TestHistoryTTLChange(t *testing.T) {
	h := New(t)
	one := h.NewNode("one")

	first := one.Submit("default lifetime")
	if got := first.ExpiresAt.Sub(first.CreatedAt); got != history.DefaultTextTTL {
		t.Fatalf("the lifetime is %s, want %s", got, history.DefaultTextTTL)
	}

	lifetime := time.Minute
	if err := one.Store().Settings().SetDuration(t.Context(), history.SettingTextTTL, lifetime); err != nil {
		t.Fatalf("store the lifetime: %v", err)
	}
	if err := one.ReloadTTLs(t.Context()); err != nil {
		t.Fatalf("reload the lifetimes: %v", err)
	}

	second := one.Submit("short lifetime")
	if got := second.ExpiresAt.Sub(second.CreatedAt); got != lifetime {
		t.Fatalf("the lifetime after the change is %s, want %s", got, lifetime)
	}
	if items, err := one.History().List(t.Context(), history.ListFilter{}); err != nil {
		t.Fatalf("list the history: %v", err)
	} else if len(items) != 2 {
		t.Fatalf("the history holds %d items, want 2", len(items))
	}

	h.Advance(2 * lifetime)
	items, err := one.History().List(t.Context(), history.ListFilter{})
	if err != nil {
		t.Fatalf("list the history: %v", err)
	}
	if len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("the history holds %d items after the expiry, want the first one only", len(items))
	}
	if _, err := one.History().Get(t.Context(), second.ID); err == nil {
		t.Fatal("the expired item is still in the history")
	}
}
