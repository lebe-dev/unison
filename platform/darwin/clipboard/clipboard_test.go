package clipboard

import (
	"testing"
	"time"
)

func TestNewDefaults(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.PollInterval(); got != DefaultPollInterval {
		t.Fatalf("PollInterval = %v, want %v", got, DefaultPollInterval)
	}
}

func TestNewCustomInterval(t *testing.T) {
	c, err := New(Options{PollInterval: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.PollInterval(); got != 50*time.Millisecond {
		t.Fatalf("PollInterval = %v, want 50ms", got)
	}
}

func TestNewRejectsNegativeInterval(t *testing.T) {
	if _, err := New(Options{PollInterval: -time.Second}); err == nil {
		t.Fatal("New with a negative interval returned no error")
	}
}
