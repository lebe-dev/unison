package keystore

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// testService is the service of the test secrets.
const testService = "app.unison.test.keystore"

// newTestSecretService returns a backend on the fake keyring.
func newTestSecretService(t *testing.T, ring *fakeKeyring, timeout time.Duration) *secretService {
	t.Helper()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	s, err := newSecretService(ring, timeout, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("newSecretService: %v", err)
	}
	return s
}

// TestSecretServiceRoundTrip stores, reads and deletes a secret.
func TestSecretServiceRoundTrip(t *testing.T) {
	ring := newFakeKeyring()
	s := newTestSecretService(t, ring, 0)
	ctx := context.Background()
	name := "identity/ed25519-seed"

	if err := s.store(ctx, testService, name, []byte("seed material")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if got := string(ring.lastValue); got != "seed material" {
		t.Fatalf("stored secret is %q, want %q", got, "seed material")
	}
	secret, err := s.load(ctx, testService, name)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(secret) != "seed material" {
		t.Fatalf("loaded secret is %q, want %q", secret, "seed material")
	}
	if err := s.remove(ctx, testService, name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.load(ctx, testService, name); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("load after delete = %v, want ErrNotFound", err)
	}
	if err := s.remove(ctx, testService, name); err != nil {
		t.Fatalf("delete of a missing secret: %v", err)
	}
}

// TestSecretServiceHidesSecretOnTheBus checks that the stored value is
// encrypted when the session uses the key exchange.
func TestSecretServiceHidesSecretOnTheBus(t *testing.T) {
	ring := newFakeKeyring()
	s := newTestSecretService(t, ring, 0)

	if err := s.store(context.Background(), testService, "store.dek", []byte("data key")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if string(ring.lastRaw) == "data key" {
		t.Fatal("the secret crossed the bus in plain text")
	}
	if len(ring.key) != aesKeyLen {
		t.Fatalf("session key is %d bytes, want %d", len(ring.key), aesKeyLen)
	}
}

// TestSecretServicePlainFallback stores through a plain session when the
// service rejects the key exchange.
func TestSecretServicePlainFallback(t *testing.T) {
	ring := newFakeKeyring()
	ring.rejectDH = true
	var log bytes.Buffer
	s, err := newSecretService(ring, 5*time.Second, slog.New(slog.NewJSONHandler(&log, nil)))
	if err != nil {
		t.Fatalf("newSecretService: %v", err)
	}
	if !ring.plain {
		t.Fatal("the service got no plain session")
	}
	if err := s.store(context.Background(), testService, "store.dek", []byte("data key")); err != nil {
		t.Fatalf("store: %v", err)
	}
	secret, err := s.load(context.Background(), testService, "store.dek")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(secret) != "data key" {
		t.Fatalf("loaded secret is %q, want %q", secret, "data key")
	}
	if !strings.Contains(log.String(), "unencrypted") {
		t.Fatalf("log does not warn about the plain session: %s", log.String())
	}
}

// TestSecretServiceItemProperties checks the label and the attributes the item
// is created with.
func TestSecretServiceItemProperties(t *testing.T) {
	ring := newFakeKeyring()
	s := newTestSecretService(t, ring, 0)
	name := "identity/mldsa65-seed"

	if err := s.store(context.Background(), testService, name, []byte("seed")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if got, want := stringProp(ring.lastProps, propLabel), "Unison "+name; got != want {
		t.Fatalf("label is %q, want %q", got, want)
	}
	attrs := attrsProp(ring.lastProps)
	want := map[string]string{attrApplication: applicationName, attrService: testService, attrAccount: name}
	for key, value := range want {
		if attrs[key] != value {
			t.Fatalf("attribute %q is %q, want %q", key, attrs[key], value)
		}
	}
}

// TestSecretServiceLoginCollection stores into the login collection when the
// default collection is missing.
func TestSecretServiceLoginCollection(t *testing.T) {
	ring := newFakeKeyring()
	ring.defaultAlias = ""
	s := newTestSecretService(t, ring, 0)

	if err := s.store(context.Background(), testService, "store.dek", []byte("key")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if ring.lastCollection != ring.loginAlias {
		t.Fatalf("created the item in %q, want the login collection %q", ring.lastCollection, ring.loginAlias)
	}
}

// TestSecretServiceNoCollection reports a keyring without a collection.
func TestSecretServiceNoCollection(t *testing.T) {
	ring := newFakeKeyring()
	ring.defaultAlias = ""
	ring.loginAlias = ""

	if _, err := newSecretService(ring, 0, slog.New(slog.DiscardHandler)); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("newSecretService = %v, want ErrKeyringUnavailable", err)
	}
}

// TestSecretServiceUnlocksCollection unlocks a locked collection through the
// prompt before storing.
func TestSecretServiceUnlocksCollection(t *testing.T) {
	ring := newFakeKeyring()
	ring.collectionLocked = true
	s := newTestSecretService(t, ring, 0)

	if err := s.store(context.Background(), testService, "store.dek", []byte("key")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if ring.collectionLocked {
		t.Fatal("the collection is still locked")
	}
	if ring.created != 1 {
		t.Fatalf("created %d items, want 1", ring.created)
	}
}

// TestSecretServiceRefusesLockedCollection reports a dismissed prompt.
func TestSecretServiceRefusesLockedCollection(t *testing.T) {
	ring := newFakeKeyring()
	ring.collectionLocked = true
	ring.dismissPrompt = true
	s := newTestSecretService(t, ring, 0)

	err := s.store(context.Background(), testService, "store.dek", []byte("key"))
	if !errors.Is(err, ErrKeyringLocked) {
		t.Fatalf("store = %v, want ErrKeyringLocked", err)
	}
	if ring.created != 0 {
		t.Fatalf("created %d items, want none", ring.created)
	}
}

// TestSecretServicePromptTimeout reports a prompt nobody answers.
func TestSecretServicePromptTimeout(t *testing.T) {
	ring := newFakeKeyring()
	ring.collectionLocked = true
	ring.silentPrompt = true
	s := newTestSecretService(t, ring, 20*time.Millisecond)

	err := s.store(context.Background(), testService, "store.dek", []byte("key"))
	if !errors.Is(err, ErrKeyringLocked) {
		t.Fatalf("store = %v, want ErrKeyringLocked", err)
	}
}

// TestSecretServiceUnlocksItem unlocks a locked item before reading it.
func TestSecretServiceUnlocksItem(t *testing.T) {
	ring := newFakeKeyring()
	ring.items[fakeItemOne] = &fakeItem{attrs: attributes(testService, "store.dek"), secret: []byte("key")}
	ring.itemLocked = true
	s := newTestSecretService(t, ring, 0)

	secret, err := s.load(context.Background(), testService, "store.dek")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(secret) != "key" {
		t.Fatalf("loaded secret is %q, want %q", secret, "key")
	}
	if ring.itemLocked {
		t.Fatal("the item is still locked")
	}
}

// TestSecretServiceCanceledContext stops before touching the keyring.
func TestSecretServiceCanceledContext(t *testing.T) {
	ring := newFakeKeyring()
	s := newTestSecretService(t, ring, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.load(ctx, testService, "store.dek"); !errors.Is(err, context.Canceled) {
		t.Fatalf("load = %v, want context.Canceled", err)
	}
	if err := s.store(ctx, testService, "store.dek", []byte("key")); !errors.Is(err, context.Canceled) {
		t.Fatalf("store = %v, want context.Canceled", err)
	}
	if ring.created != 0 {
		t.Fatalf("created %d items, want none", ring.created)
	}
}

// TestSessionHidesDH checks that the shared key of the two sides matches.
func TestSessionHidesDH(t *testing.T) {
	clientPrivate, clientPublic, err := dhKeyPair()
	if err != nil {
		t.Fatalf("dhKeyPair: %v", err)
	}
	serverPrivate, serverPublic, err := dhKeyPair()
	if err != nil {
		t.Fatalf("dhKeyPair: %v", err)
	}
	clientKey := dhSharedKey(clientPrivate, serverPublic)
	serverKey := dhSharedKey(serverPrivate, clientPublic)
	if !bytes.Equal(clientKey, serverKey) {
		t.Fatal("the two sides computed different keys")
	}
	if len(clientKey) != aesKeyLen {
		t.Fatalf("key is %d bytes, want %d", len(clientKey), aesKeyLen)
	}
}

// TestDHPrime checks the group of the key exchange.
func TestDHPrime(t *testing.T) {
	if got := dhPrime.BitLen(); got != 1024 {
		t.Fatalf("prime is %d bits, want 1024", got)
	}
}

// TestPKCS7 checks the padding of the session encryption.
func TestPKCS7(t *testing.T) {
	cases := [][]byte{nil, []byte("a"), bytes.Repeat([]byte("x"), 15), bytes.Repeat([]byte("x"), 16)}
	for _, data := range cases {
		padded := pkcs7Pad(data, 16)
		if len(padded)%16 != 0 {
			t.Fatalf("padded %d bytes to %d", len(data), len(padded))
		}
		got, err := pkcs7Unpad(padded, 16)
		if err != nil {
			t.Fatalf("pkcs7Unpad: %v", err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("unpad returned %q, want %q", got, data)
		}
	}
	if _, err := pkcs7Unpad([]byte("short"), 16); err == nil {
		t.Fatal("pkcs7Unpad accepted a short block")
	}
	if _, err := pkcs7Unpad(bytes.Repeat([]byte{0}, 16), 16); err == nil {
		t.Fatal("pkcs7Unpad accepted an invalid padding")
	}
}
