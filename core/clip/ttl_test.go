package clip

import (
	"testing"
	"time"
)

// TestSetTTLsAppliesToNextItem checks that a changed lifetime is used by the next
// captured item and that the empty kinds keep the defaults.
func TestSetTTLsAppliesToNextItem(t *testing.T) {
	f := newFixture(t)

	f.emit(textSnapshot("first", ""))
	first := f.waitItem()
	if got := first.ExpiresAt.Sub(first.CreatedAt); got != DefaultTextTTL {
		t.Fatalf("the lifetime is %s, want %s", got, DefaultTextTTL)
	}

	lifetime := time.Hour
	f.pipe.SetTTLs(TTLs{Text: lifetime})

	want := TTLs{Text: lifetime, Image: DefaultImageTTL, Files: DefaultFilesTTL}
	if got := f.pipe.TTLs(); got != want {
		t.Fatalf("the lifetimes are %+v, want %+v", got, want)
	}

	f.emit(textSnapshot("second", ""))
	second := f.waitItem()
	if got := second.ExpiresAt.Sub(second.CreatedAt); got != lifetime {
		t.Fatalf("the lifetime of the next item is %s, want %s", got, lifetime)
	}
}
