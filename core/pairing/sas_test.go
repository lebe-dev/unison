package pairing

import (
	"slices"
	"strings"
	"testing"
)

// TestSASVectors pins the derivation of the short authentication string.
func TestSASVectors(t *testing.T) {
	one := testIdentity(t, 1)
	other := testIdentity(t, 2)

	sas, err := computeSAS(testEKM(), one.Public(), other.Public())
	if err != nil {
		t.Fatalf("compute sas: %v", err)
	}
	if got, want := sas.Digits, "945349"; got != want {
		t.Errorf("digits = %q, want %q", got, want)
	}
	want := []string{"🐙", "🐄", "🐮", "🌵"}
	if !slices.Equal(sas.Emoji, want) {
		t.Errorf("emoji = %v, want %v", sas.Emoji, want)
	}
	if got, want := sas.String(), "🐙🐄🐮🌵 945349"; got != want {
		t.Errorf("string = %q, want %q", got, want)
	}

	// The device order must not change the result.
	reversed, err := computeSAS(testEKM(), other.Public(), one.Public())
	if err != nil {
		t.Fatalf("compute reversed sas: %v", err)
	}
	if reversed.String() != sas.String() {
		t.Errorf("reversed sas = %q, want %q", reversed, sas)
	}
}

// TestSASDependsOnExporter checks that another TLS session yields another SAS,
// which is what exposes a device in the middle.
func TestSASDependsOnExporter(t *testing.T) {
	one := testIdentity(t, 1)
	other := testIdentity(t, 2)

	first, err := computeSAS(testEKM(), one.Public(), other.Public())
	if err != nil {
		t.Fatalf("compute sas: %v", err)
	}
	ekm := testEKM()
	ekm[0] ^= 0xff
	second, err := computeSAS(ekm, one.Public(), other.Public())
	if err != nil {
		t.Fatalf("compute sas: %v", err)
	}
	if first.String() == second.String() {
		t.Fatalf("another session produced the same sas %q", first)
	}
}

// TestSASShape checks the shape of the string the user compares.
func TestSASShape(t *testing.T) {
	one := testIdentity(t, 1)
	other := testIdentity(t, 2)

	sas, err := computeSAS(testEKM(), one.Public(), other.Public())
	if err != nil {
		t.Fatalf("compute sas: %v", err)
	}
	if len(sas.Digits) != sasDigitCount {
		t.Errorf("digits %q are %d long, want %d", sas.Digits, len(sas.Digits), sasDigitCount)
	}
	if strings.Trim(sas.Digits, "0123456789") != "" {
		t.Errorf("digits %q are not decimal", sas.Digits)
	}
	if len(sas.Emoji) != sasEmojiCount {
		t.Errorf("emoji count = %d, want %d", len(sas.Emoji), sasEmojiCount)
	}
	if sas.IsZero() {
		t.Error("sas is zero")
	}
	if _, err := computeSAS([]byte{1, 2, 3}, one.Public(), other.Public()); err == nil {
		t.Error("short exporter key material was accepted")
	}
}

// TestEmojiTable checks that the table is complete and has no duplicates.
func TestEmojiTable(t *testing.T) {
	seen := make(map[string]int, len(emojiTable))
	for i, emoji := range emojiTable {
		if emoji == "" {
			t.Fatalf("emoji %d is empty", i)
		}
		if first, ok := seen[emoji]; ok {
			t.Fatalf("emoji %d repeats emoji %d", i, first)
		}
		seen[emoji] = i
	}
}
