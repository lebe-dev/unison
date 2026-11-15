package store

import (
	"bytes"
	"errors"
	"testing"
)

// testSealer returns a sealer for a fixed key.
func testSealer(t *testing.T) *Sealer {
	t.Helper()

	sealer, err := newSealer(bytes.Repeat([]byte{7}, dekSize))
	if err != nil {
		t.Fatalf("create sealer: %v", err)
	}
	return sealer
}

func TestSealOpenRoundTrip(t *testing.T) {
	sealer := testSealer(t)

	payloads := map[string][]byte{
		"empty": nil,
		"text":  []byte("hello"),
		"binary": {
			0, 1, 2, 3, 0xff,
		},
		"large": bytes.Repeat([]byte("unison"), 4096),
	}
	for name, plaintext := range payloads {
		t.Run(name, func(t *testing.T) {
			sealed, err := sealer.Seal("history", "row-1", plaintext)
			if err != nil {
				t.Fatalf("seal: %v", err)
			}
			if len(plaintext) > 0 && bytes.Contains(sealed, plaintext) {
				t.Fatal("sealed value contains the plaintext")
			}

			opened, err := sealer.Open("history", "row-1", sealed)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if !bytes.Equal(opened, plaintext) {
				t.Fatalf("open: got %x, want %x", opened, plaintext)
			}
		})
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	sealer := testSealer(t)
	plaintext := []byte("same plaintext")

	first, err := sealer.Seal("settings", "k", plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := sealer.Seal("settings", "k", plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two seals of the same plaintext are equal")
	}
}

func TestSealBindsTableAndRowKey(t *testing.T) {
	sealer := testSealer(t)
	sealed, err := sealer.Seal("history", "row-1", []byte("payload"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	tests := []struct {
		name  string
		table string
		row   string
	}{
		{name: "other table", table: "settings", row: "row-1"},
		{name: "other row", table: "history", row: "row-2"},
		{name: "other table and row", table: "peers", row: "row-2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := sealer.Open(test.table, test.row, sealed); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("open with other binding: got %v, want ErrDecrypt", err)
			}
		})
	}
}

func TestSealRejectsTamperedCiphertext(t *testing.T) {
	sealer := testSealer(t)
	sealed, err := sealer.Seal("history", "row-1", []byte("payload"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := sealer.Open("history", "row-1", tampered); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("open tampered value: got %v, want ErrDecrypt", err)
	}

	short := sealed[:len(sealed)-1]
	if _, err := sealer.Open("history", "row-1", short); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("open truncated value: got %v, want ErrDecrypt", err)
	}

	if _, err := sealer.Open("history", "row-1", nil); !errors.Is(err, ErrCiphertext) {
		t.Fatalf("open empty value: got %v, want ErrCiphertext", err)
	}
}

func TestSealerRejectsWrongKeySize(t *testing.T) {
	if _, err := newSealer([]byte("short")); err == nil {
		t.Fatal("sealer for a short key: want an error")
	}
}

func TestSealedValuesOfDifferentStoresDiffer(t *testing.T) {
	first, err := newSealer(bytes.Repeat([]byte{1}, dekSize))
	if err != nil {
		t.Fatalf("create sealer: %v", err)
	}
	second, err := newSealer(bytes.Repeat([]byte{2}, dekSize))
	if err != nil {
		t.Fatalf("create sealer: %v", err)
	}

	sealed, err := first.Seal("settings", "k", []byte("payload"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := second.Open("settings", "k", sealed); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("open with another key: got %v, want ErrDecrypt", err)
	}
}
