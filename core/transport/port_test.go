package transport

import (
	"context"
	"testing"
)

// mapSettings is an in-memory Settings.
type mapSettings map[string]int64

// GetInt returns the stored value or the default.
func (s mapSettings) GetInt(_ context.Context, key string, def int64) (int64, error) {
	if value, ok := s[key]; ok {
		return value, nil
	}
	return def, nil
}

// SetInt stores a value.
func (s mapSettings) SetInt(_ context.Context, key string, value int64) error {
	s[key] = value
	return nil
}

// TestMainPortIsGeneratedOnce checks that the listener port is picked once and
// then kept.
func TestMainPortIsGeneratedOnce(t *testing.T) {
	ctx := context.Background()
	settings := mapSettings{}

	port, err := MainPort(ctx, settings)
	if err != nil {
		t.Fatalf("main port: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port = %d, want a valid port", port)
	}
	if got := settings[PortSettingsKey]; got != int64(port) {
		t.Fatalf("stored port = %d, want %d", got, port)
	}

	again, err := MainPort(ctx, settings)
	if err != nil {
		t.Fatalf("second main port: %v", err)
	}
	if again != port {
		t.Fatalf("second port = %d, want the stored %d", again, port)
	}
}

// TestMainPortKeepsStoredValue checks that a stored port wins over a new pick.
func TestMainPortKeepsStoredValue(t *testing.T) {
	ctx := context.Background()
	settings := mapSettings{PortSettingsKey: 45876}

	port, err := MainPort(ctx, settings)
	if err != nil {
		t.Fatalf("main port: %v", err)
	}
	if port != 45876 {
		t.Fatalf("port = %d, want 45876", port)
	}
}

// TestMainPortReplacesInvalidValue checks that a port out of range is replaced.
func TestMainPortReplacesInvalidValue(t *testing.T) {
	ctx := context.Background()
	settings := mapSettings{PortSettingsKey: 70000}

	port, err := MainPort(ctx, settings)
	if err != nil {
		t.Fatalf("main port: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port = %d, want a valid port", port)
	}
	if got := settings[PortSettingsKey]; got != int64(port) {
		t.Fatalf("stored port = %d, want %d", got, port)
	}
}

// TestMainPortWithoutSettings checks that a missing settings store is refused.
func TestMainPortWithoutSettings(t *testing.T) {
	if _, err := MainPort(context.Background(), nil); err == nil {
		t.Fatal("main port without settings was returned")
	}
}
