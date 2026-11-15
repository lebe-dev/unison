package model

import (
	"strings"
	"testing"
)

func TestDeviceIDStringAndParse(t *testing.T) {
	var id DeviceID
	for i := range id {
		id[i] = byte(i)
	}

	encoded := id.String()
	if len(encoded) != 64 {
		t.Fatalf("hex length: got %d, want 64", len(encoded))
	}
	parsed, err := ParseDeviceID(encoded)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed != id {
		t.Fatalf("parse: got %v, want %v", parsed, id)
	}

	if _, err := ParseDeviceID("0000"); err == nil {
		t.Fatal("parse short id: expected an error")
	}
	if _, err := ParseDeviceID(strings.Repeat("zz", 32)); err == nil {
		t.Fatal("parse non hex id: expected an error")
	}
}

func TestDeviceIDFromBytes(t *testing.T) {
	id, err := DeviceIDFromBytes(make([]byte, 32))
	if err != nil {
		t.Fatalf("from bytes: %v", err)
	}
	if !id.IsZero() {
		t.Fatal("zero bytes gave a non zero id")
	}
	if _, err := DeviceIDFromBytes(make([]byte, 31)); err == nil {
		t.Fatal("from 31 bytes: expected an error")
	}
}

func TestFingerprintOf(t *testing.T) {
	var id DeviceID
	for i := range id {
		id[i] = byte(i)
	}
	if got, want := FingerprintOf(id), Fingerprint("AAAQ-EAYE-AUDA-OCAJ"); got != want {
		t.Fatalf("fingerprint: got %q, want %q", got, want)
	}

	var full DeviceID
	for i := range full {
		full[i] = 0xff
	}
	if got, want := FingerprintOf(full), Fingerprint("7777-7777-7777-7777"); got != want {
		t.Fatalf("fingerprint: got %q, want %q", got, want)
	}
}

func TestClipIDStringAndParse(t *testing.T) {
	var id ClipID
	for i := range id {
		id[i] = byte(i)
	}
	const encoded = "00010203-0405-0607-0809-0a0b0c0d0e0f"
	if got := id.String(); got != encoded {
		t.Fatalf("string: got %q, want %q", got, encoded)
	}

	parsed, err := ParseClipID(encoded)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed != id {
		t.Fatalf("parse: got %v, want %v", parsed, id)
	}
	if _, err := ParseClipID("000102030405060708090a0b0c0d0e0f"); err != nil {
		t.Fatalf("parse without dashes: %v", err)
	}
	if _, err := ParseClipID("nope"); err == nil {
		t.Fatal("parse invalid id: expected an error")
	}
}
